package httpapi

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/rs/zerolog/log"
)

// ============================================================================
// Rate Limiting with Token Bucket Algorithm
// ============================================================================
//
// PATTERN: Per-user token bucket for smooth, fair rate limiting
//
// The token bucket algorithm allows:
// - Burst traffic up to capacity (good UX for interactive clients)
// - Smooth long-term rate limiting (no thundering herd at window boundaries)
// - Per-user fairness (one user can't starve others)
//
// Configuration:
//   RateLimitInfo{
//     WindowSeconds: 60,   // 1 minute window
//     MaxRequests:   600,  // 600 requests per window
//     Burst:         120,  // Allow 120 request burst
//   }
//   => Refill rate: 600/60 = 10 tokens/second
//
// Algorithm:
//   1. On request: calculate elapsed time since last refill
//   2. Add (elapsed * refillRate) tokens, capped at capacity
//   3. If tokens >= 1.0: consume 1, allow request
//   4. Else: calculate wait time, return 429 with Retry-After
//
// Production Note:
//   Current implementation uses in-memory map[userID]*TokenBucket.
//   For distributed systems, replace with Redis-backed rate limiter.
//
// See: docs/sync_phase7_design_patterns.md for full pattern documentation
// ============================================================================

// TokenBucket implements a token bucket rate limiter
type TokenBucket struct {
	tokens     float64
	capacity   float64
	refillRate float64 // tokens per second
	lastRefill time.Time
	mu         sync.Mutex
}

// NewTokenBucket creates a new token bucket with given capacity and refill rate
func NewTokenBucket(capacity int, refillRate float64) *TokenBucket {
	return &TokenBucket{
		tokens:     float64(capacity),
		capacity:   float64(capacity),
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

// Allow checks if a token is available and consumes it if so
// Returns (allowed bool, tokensRemaining int, nextTokenTime time.Time, fullResetTime time.Time)
// - nextTokenTime: when the next token will be available (use for Retry-After)
// - fullResetTime: when the bucket will be completely full (use for X-RateLimit-Reset)
func (tb *TokenBucket) Allow() (bool, int, time.Time, time.Time) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}
	tb.lastRefill = now

	// Calculate full reset time (when bucket will be completely full)
	tokensNeeded := tb.capacity - tb.tokens
	fullResetTime := now.Add(time.Duration(tokensNeeded/tb.refillRate) * time.Second)

	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		// Next token available immediately (we just consumed one but more available)
		return true, int(tb.tokens), now, fullResetTime
	}

	// Calculate when next token will be available (not when bucket is full)
	tokensUntilNext := 1.0 - tb.tokens
	secondsUntilNext := tokensUntilNext / tb.refillRate
	nextTokenTime := now.Add(time.Duration(secondsUntilNext) * time.Second)

	return false, 0, nextTokenTime, fullResetTime
}

// RateLimiter manages per-user token buckets
type RateLimiter struct {
	buckets map[string]*TokenBucket
	config  RateLimitInfo
	mu      sync.RWMutex
}

// NewRateLimiter creates a new rate limiter with the given configuration
func NewRateLimiter(config RateLimitInfo) *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*TokenBucket),
		config:  config,
	}

	// Start cleanup goroutine to remove inactive buckets
	go rl.cleanupLoop()

	return rl
}

// getBucket retrieves or creates a token bucket for the given user
func (rl *RateLimiter) getBucket(userID string) *TokenBucket {
	rl.mu.RLock()
	bucket, exists := rl.buckets[userID]
	rl.mu.RUnlock()

	if exists {
		return bucket
	}

	// Create new bucket
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock
	if bucket, exists := rl.buckets[userID]; exists {
		return bucket
	}

	refillRate := float64(rl.config.MaxRequests) / float64(rl.config.WindowSeconds)
	bucket = NewTokenBucket(rl.config.Burst, refillRate)
	rl.buckets[userID] = bucket
	return bucket
}

// Allow checks if the user is allowed to make a request
// Returns (allowed bool, remaining int, nextTokenTime time.Time, fullResetTime time.Time)
func (rl *RateLimiter) Allow(userID string) (bool, int, time.Time, time.Time) {
	bucket := rl.getBucket(userID)
	return bucket.Allow()
}

// cleanupLoop periodically removes inactive buckets to prevent memory leaks
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		for userID, bucket := range rl.buckets {
			bucket.mu.Lock()
			// Remove bucket if it hasn't been used in the last hour
			if time.Since(bucket.lastRefill) > time.Hour {
				delete(rl.buckets, userID)
			}
			bucket.mu.Unlock()
		}
		rl.mu.Unlock()
	}
}

// RateLimitMiddleware returns a middleware that enforces rate limiting per user
// Each middleware instance creates its own rate limiter with the provided configuration,
// allowing different routes to have different rate limits.
// Production Note: For distributed systems, replace with Redis-backed rate limiter.
func RateLimitMiddleware(config RateLimitInfo) func(http.Handler) http.Handler {
	// Create a dedicated rate limiter for this middleware instance
	// This allows different routes to have different rate limits
	limiter := NewRateLimiter(config)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get user ID from context (set by auth middleware)
			userID := auth.UserID(r.Context())
			if userID == "" {
				// No user ID means unauthenticated request, skip rate limiting
				next.ServeHTTP(w, r)
				return
			}

			// Check rate limit
			allowed, remaining, nextTokenTime, fullResetTime := limiter.Allow(userID)

			// Set rate limit headers
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(config.MaxRequests))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(fullResetTime.Unix(), 10))
			w.Header().Set("X-RateLimit-Burst", strconv.Itoa(config.Burst))

			if !allowed {
				// Calculate Retry-After in seconds (time until next token available)
				retryAfter := int(time.Until(nextTokenTime).Seconds())
				if retryAfter < 1 {
					retryAfter = 1
				}

				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))

				log.Warn().
					Str("userId", userID).
					Str("path", r.URL.Path).
					Int("retryAfter", retryAfter).
					Msg("Rate limit exceeded")

				writeError(w, r, http.StatusTooManyRequests,
					"Rate limit exceeded. Please retry after "+strconv.Itoa(retryAfter)+" seconds.")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
