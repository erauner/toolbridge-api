package httpapi

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/rs/zerolog/log"
)

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
// Returns (allowed bool, tokensRemaining int, resetTime time.Time)
func (tb *TokenBucket) Allow() (bool, int, time.Time) {
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

	// Calculate reset time (when bucket will be full again)
	tokensNeeded := tb.capacity - tb.tokens
	resetTime := now.Add(time.Duration(tokensNeeded/tb.refillRate) * time.Second)

	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true, int(tb.tokens), resetTime
	}

	// Calculate retry-after (seconds until next token available)
	return false, 0, resetTime
}

// RateLimiter manages per-user token buckets
type RateLimiter struct {
	buckets map[string]*TokenBucket
	config  RateLimitInfo
	mu      sync.RWMutex
}

// NewRateLimiter creates a new rate limiter with the given configuration
func NewRateLimiter(config RateLimitInfo) *RateLimiter {
	// Calculate refill rate: maxRequests per windowSeconds
	refillRate := float64(config.MaxRequests) / float64(config.WindowSeconds)

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
func (rl *RateLimiter) Allow(userID string) (bool, int, time.Time) {
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

// Global rate limiter instance (in production, use Redis or similar for distributed systems)
var globalRateLimiter *RateLimiter

// initRateLimiter initializes the global rate limiter (called from router setup)
func initRateLimiter(config RateLimitInfo) {
	globalRateLimiter = NewRateLimiter(config)
}

// RateLimitMiddleware returns a middleware that enforces rate limiting per user
func RateLimitMiddleware(config RateLimitInfo) func(http.Handler) http.Handler {
	// Initialize global rate limiter if not already done
	if globalRateLimiter == nil {
		initRateLimiter(config)
	}

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
			allowed, remaining, resetTime := globalRateLimiter.Allow(userID)

			// Set rate limit headers
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(config.MaxRequests))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime.Unix(), 10))

			if !allowed {
				// Calculate Retry-After in seconds
				retryAfter := int(time.Until(resetTime).Seconds())
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
