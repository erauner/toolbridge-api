package auth

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/erauner12/toolbridge-api/internal/mcpserver/config"
	"github.com/rs/zerolog/log"
)

const (
	// ExpiryBuffer is the time before token expiry to trigger refresh (5 minutes)
	ExpiryBuffer = 5 * time.Minute
)

// TokenBroker manages Auth0 token acquisition and caching
type TokenBroker struct {
	mu            sync.RWMutex
	config        config.Auth0Config
	delegate      Delegate
	cache         map[string]*CachedToken
	defaultScopes []string
}

// CachedToken wraps TokenResult with caching metadata
type CachedToken struct {
	Token    *TokenResult
	CachedAt time.Time
}

// NewBroker creates a new token broker
func NewBroker(cfg config.Auth0Config, delegate Delegate) (*TokenBroker, error) {
	if err := delegate.Configure(cfg); err != nil {
		return nil, fmt.Errorf("failed to configure delegate: %w", err)
	}

	defaultScopes := cfg.GetDefaultScopes()

	log.Debug().
		Strs("defaultScopes", defaultScopes).
		Msg("token broker initialized")

	return &TokenBroker{
		config:        cfg,
		delegate:      delegate,
		cache:         make(map[string]*CachedToken),
		defaultScopes: defaultScopes,
	}, nil
}

// GetToken acquires an access token for the given audience
// interactive: whether to allow user interaction (device code display)
// scope: additional scopes to request (will be merged with default scopes)
func (b *TokenBroker) GetToken(ctx context.Context, audience, scope string, interactive bool) (*TokenResult, error) {
	// Merge scopes (default + user-provided, deduplicated, sorted)
	mergedScopes := b.mergeScopes(scope)

	// Check cache
	cacheKey := b.cacheKey(audience, mergedScopes)
	if cached := b.getCached(cacheKey); cached != nil && !b.isExpiring(cached.Token) {
		log.Debug().
			Str("audience", audience).
			Str("cacheKey", cacheKey).
			Time("expiresAt", cached.Token.ExpiresAt).
			Msg("returning cached token")
		return cached.Token, nil
	}

	// Ensure session exists (silent if not interactive)
	if _, err := b.delegate.EnsureSession(ctx, false, b.defaultScopes); err != nil {
		return nil, fmt.Errorf("failed to ensure session: %w", err)
	}

	// Try to get token
	token, err := b.delegate.TryGetToken(ctx, audience, mergedScopes, interactive)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire token: %w", err)
	}
	if token == nil {
		return nil, ErrTokenAcquisitionFailed{
			Audience:    audience,
			Interactive: interactive,
			Reason:      "delegate returned nil token",
		}
	}

	// Cache and return
	b.setCached(cacheKey, token)
	log.Info().
		Str("audience", audience).
		Time("expiresAt", token.ExpiresAt).
		Msg("acquired new token")

	return token, nil
}

// InvalidateToken removes a token from cache (e.g., on 401)
func (b *TokenBroker) InvalidateToken(audience, scope string) {
	mergedScopes := b.mergeScopes(scope)
	cacheKey := b.cacheKey(audience, mergedScopes)

	b.mu.Lock()
	delete(b.cache, cacheKey)
	b.mu.Unlock()

	log.Debug().
		Str("audience", audience).
		Str("cacheKey", cacheKey).
		Msg("invalidated cached token")
}

// GetIDToken gets an ID token for user info
func (b *TokenBroker) GetIDToken(ctx context.Context) (string, error) {
	return b.delegate.TryGetIDToken(ctx, b.defaultScopes)
}

// LogoutAll clears all cached tokens and logs out
func (b *TokenBroker) LogoutAll(ctx context.Context) error {
	b.mu.Lock()
	b.cache = make(map[string]*CachedToken)
	b.mu.Unlock()

	return b.delegate.LogoutAll(ctx)
}

// mergeScopes merges default scopes with user-provided scopes
// Returns deduplicated, sorted list
func (b *TokenBroker) mergeScopes(scope string) []string {
	scopeSet := make(map[string]bool)

	// Add default scopes
	for _, s := range b.defaultScopes {
		if s != "" {
			scopeSet[s] = true
		}
	}

	// Add user-provided scopes
	if scope != "" {
		userScopes := strings.Fields(scope)
		for _, s := range userScopes {
			if s != "" {
				scopeSet[s] = true
			}
		}
	}

	// Convert to sorted slice
	scopes := make([]string, 0, len(scopeSet))
	for s := range scopeSet {
		scopes = append(scopes, s)
	}
	sort.Strings(scopes)

	return scopes
}

// cacheKey generates a cache key for the given audience and scopes
// Format: "audience::scope1 scope2 scope3"
func (b *TokenBroker) cacheKey(audience string, scopes []string) string {
	scopeStr := strings.Join(scopes, " ")
	return fmt.Sprintf("%s::%s", audience, scopeStr)
}

// isExpiring returns true if token expires within ExpiryBuffer (5 minutes)
func (b *TokenBroker) isExpiring(token *TokenResult) bool {
	timeUntilExpiry := time.Until(token.ExpiresAt)
	isExpiring := timeUntilExpiry <= ExpiryBuffer

	if isExpiring {
		log.Debug().
			Time("expiresAt", token.ExpiresAt).
			Dur("timeUntilExpiry", timeUntilExpiry).
			Msg("token is expiring soon")
	}

	return isExpiring
}

// getCached retrieves a token from cache (thread-safe)
func (b *TokenBroker) getCached(key string) *CachedToken {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.cache[key]
}

// setCached stores a token in cache (thread-safe)
func (b *TokenBroker) setCached(key string, token *TokenResult) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.cache[key] = &CachedToken{
		Token:    token,
		CachedAt: time.Now(),
	}

	log.Debug().
		Str("cacheKey", key).
		Time("expiresAt", token.ExpiresAt).
		Msg("cached token")
}
