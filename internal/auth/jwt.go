package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

type ctxKey string

const (
	CtxUserID  ctxKey = "uid" // Database user ID (app_user.id)
	CtxSubject ctxKey = "sub" // OIDC subject claim (JWT sub, WorkOS user ID)
)

// JWTCfg holds JWT authentication configuration
type JWTCfg struct {
	HS256Secret       string   // HMAC secret for HS256 tokens (dev/testing)
	DevMode           bool     // Allow X-Debug-Sub header (DANGEROUS: only for local dev)
	Issuer            string   // Upstream IdP issuer (e.g., "https://your-app.authkit.app")
	JWKSURL           string   // JWKS endpoint URL (e.g., "https://your-app.authkit.app/oauth2/jwks")
	Audience          string   // Optional primary expected audience claim
	AcceptedAudiences []string // Additional accepted audiences (for MCP OAuth tokens, backend tokens, etc.)

	// TenantClaim: JWT claim key for tenant/organization ID (e.g., "organization_id")
	//
	// TENANT IDENTITY CONTRACT:
	// This claim should contain the IdP-level organization ID (from WorkOS, Okta, Auth0, etc.),
	// NOT a database-specific identifier like a Neon branch ID or schema name.
	//
	// The value extracted from this claim will be used as:
	//   1. The tenant_id stored in request context (via TenantID(ctx))
	//   2. The key for future tenant_registry lookups (Neon DB-per-tenant routing)
	//   3. The logical tenant identifier across all system components
	//
	// DO NOT change this to extract database routing information from JWT claims.
	// Database topology should be determined server-side via tenant_registry, not client-provided.
	//
	// See: Plans/neon-migration-tenant-contract.md
	TenantClaim string
}

// JWKS caching for upstream IdP public keys
type jwksCache struct {
	mu         sync.RWMutex
	keys       map[string]*rsa.PublicKey
	lastFetch  time.Time
	cacheTTL   time.Duration
	jwksURL    string        // Explicit JWKS URL instead of deriving from domain
	httpClient *http.Client // HTTP client with timeout for JWKS fetching
}

var globalJWKSCache *jwksCache

// JWKS response structure from OIDC provider
type jwksResponse struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// fetchJWKS fetches and caches public keys from upstream IdP for RS256 validation
// If forceRefresh is true, bypasses TTL check to handle key rotations
func (c *jwksCache) fetchJWKS(forceRefresh bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Return cached keys if still fresh (unless force refresh requested)
	if !forceRefresh && time.Since(c.lastFetch) < c.cacheTTL && len(c.keys) > 0 {
		return nil
	}

	resp, err := c.httpClient.Get(c.jwksURL)
	if err != nil {
		return fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read JWKS response: %w", err)
	}

	var jwks jwksResponse
	if err := json.Unmarshal(body, &jwks); err != nil {
		return fmt.Errorf("failed to parse JWKS: %w", err)
	}

	// Convert JWKs to RSA public keys
	keys := make(map[string]*rsa.PublicKey)
	for _, key := range jwks.Keys {
		if key.Kty != "RSA" || key.Use != "sig" {
			continue
		}

		// Decode modulus (n) and exponent (e) from base64url
		nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
		if err != nil {
			log.Warn().Err(err).Str("kid", key.Kid).Msg("failed to decode modulus")
			continue
		}

		eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
		if err != nil {
			log.Warn().Err(err).Str("kid", key.Kid).Msg("failed to decode exponent")
			continue
		}

		// Convert exponent bytes to int
		var eInt int
		for _, b := range eBytes {
			eInt = eInt<<8 | int(b)
		}

		pubKey := &rsa.PublicKey{
			N: new(big.Int).SetBytes(nBytes),
			E: eInt,
		}

		keys[key.Kid] = pubKey
	}

	if len(keys) == 0 {
		return errors.New("no valid RSA signing keys found in JWKS")
	}

	c.keys = keys
	c.lastFetch = time.Now()
	log.Info().Int("key_count", len(keys)).Msg("refreshed JWKS cache")

	return nil
}

// getPublicKey retrieves a cached public key by kid (key ID)
func (c *jwksCache) getPublicKey(kid string) (*rsa.PublicKey, error) {
	// Check if cache has expired (before checking if key exists)
	// This ensures we refresh even when the requested key is present
	c.mu.RLock()
	cacheExpired := time.Since(c.lastFetch) >= c.cacheTTL
	c.mu.RUnlock()

	if cacheExpired {
		// Cache expired - refresh JWKS to detect key rotations/revocations
		if err := c.fetchJWKS(false); err != nil {
			// Log error but don't fail - continue with stale cache as fallback
			log.Warn().Err(err).Msg("failed to refresh expired JWKS cache, using stale keys")
		}
	}

	c.mu.RLock()
	key, ok := c.keys[kid]
	c.mu.RUnlock()

	if !ok {
		// Key not found in cache - force refresh to handle OIDC provider key rotation
		// Even if cache is fresh, we need to fetch new keys when kid is missing
		if err := c.fetchJWKS(true); err != nil {
			return nil, fmt.Errorf("failed to fetch JWKS for missing key %s: %w", kid, err)
		}

		c.mu.RLock()
		key, ok = c.keys[kid]
		c.mu.RUnlock()

		if !ok {
			return nil, fmt.Errorf("key ID %s not found in JWKS even after refresh", kid)
		}
	}

	return key, nil
}

// ValidateToken validates a JWT token and returns the subject claim and claims map
// Returns the subject (sub) claim and full claims map if valid, or an error if validation fails
// Supports both RS256 (upstream IdP / WorkOS) and HS256 (backend / dev) tokens
func ValidateToken(tokenString string, cfg JWTCfg) (string, jwt.MapClaims, error) {
	if tokenString == "" {
		return "", nil, errors.New("token is empty")
	}

	// Ensure JWKS cache is initialized if upstream IdP is configured
	if cfg.JWKSURL != "" && globalJWKSCache == nil {
		return "", nil, errors.New("JWKS cache not initialized")
	}

	claims := jwt.MapClaims{}
	t, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		// Support both RS256 (upstream IdP) and HS256 (backend / dev)
		switch t.Method.(type) {
		case *jwt.SigningMethodRSA:
			// RS256 token from upstream IdP - fetch public key from JWKS
			if globalJWKSCache == nil {
				return nil, errors.New("JWKS cache not initialized")
			}

			// Extract kid (key ID) from token header
			kid, ok := t.Header["kid"].(string)
			if !ok || kid == "" {
				return nil, errors.New("missing kid in token header")
			}

			// Get public key from JWKS
			pubKey, err := globalJWKSCache.getPublicKey(kid)
			if err != nil {
				return nil, fmt.Errorf("failed to get public key: %w", err)
			}

			return pubKey, nil

		case *jwt.SigningMethodHMAC:
			// HS256 token (backend / dev) - use shared secret
			if cfg.HS256Secret == "" {
				return nil, errors.New("HS256 secret not configured")
			}
			return []byte(cfg.HS256Secret), nil

		default:
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
	})

	if err != nil || !t.Valid {
		return "", nil, fmt.Errorf("jwt validation failed: %w", err)
	}

	// Extract token_type to differentiate backend tokens from external IdP tokens
	tokenType, _ := claims["token_type"].(string)
	issuer, _ := claims["iss"].(string)

	// Backend tokens: Skip issuer/audience checks (internal tokens with token_type="backend")
	// Legacy backend tokens: HS256 tokens with iss="toolbridge-api" but no token_type claim
	// External tokens: Validate issuer and audience against upstream IdP config
	isBackendToken := tokenType == "backend" || (tokenType == "" && issuer == "toolbridge-api")

	if isBackendToken {
		// Backend token (new or legacy) - validated by signature, no additional checks needed
	} else {
		// External IdP token (WorkOS AuthKit, etc.) - validate issuer and audience
		if cfg.Issuer != "" {
			if iss, ok := claims["iss"].(string); !ok || iss != cfg.Issuer {
				return "", nil, fmt.Errorf("invalid issuer: expected %s, got %v", cfg.Issuer, claims["iss"])
			}
		}

		// Validate audience if configured
		// Accepts primary audience OR any of the additional accepted audiences
		//
		// Special case: Skip audience validation for WorkOS AuthKit when using DCR
		// (Dynamic Client Registration). With DCR, each client gets a unique client ID
		// as the audience, which is unpredictable. We only validate issuer + signature.
		// DCR mode is enabled when BOTH:
		// - MCP_OAUTH_AUDIENCE is empty (AcceptedAudiences is empty)
		// - JWT_AUDIENCE is also empty (Audience is empty)
		// This ensures audience-based isolation is enforced when JWT_AUDIENCE is configured.
		skipAudienceValidation := cfg.Issuer != "" && issuer == cfg.Issuer &&
			len(cfg.AcceptedAudiences) == 0 && cfg.Audience == ""

		if !skipAudienceValidation && (cfg.Audience != "" || len(cfg.AcceptedAudiences) > 0) {
			// Build list of all accepted audiences
			acceptedAuds := []string{}
			if cfg.Audience != "" {
				acceptedAuds = append(acceptedAuds, cfg.Audience)
			}
			if len(cfg.AcceptedAudiences) > 0 {
				acceptedAuds = append(acceptedAuds, cfg.AcceptedAudiences...)
			}

			audValid := false
			switch aud := claims["aud"].(type) {
			case string:
				// Token has single audience - check if it matches any accepted audience
				for _, accepted := range acceptedAuds {
					if aud == accepted {
						audValid = true
						break
					}
				}
			case []interface{}:
				// Token has multiple audiences - check if any matches accepted audiences
				for _, a := range aud {
					if s, ok := a.(string); ok {
						for _, accepted := range acceptedAuds {
							if s == accepted {
								audValid = true
								break
							}
						}
						if audValid {
							break
						}
					}
				}
			}
			if !audValid {
				return "", nil, fmt.Errorf("invalid audience: expected one of %v, got %v", acceptedAuds, claims["aud"])
			}
		}
	}

	// Extract subject from claims
	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		return "", nil, errors.New("missing or invalid sub claim")
	}

	return sub, claims, nil
}

// InitJWKSCache initializes the global JWKS cache for upstream IdP RS256 validation
// Should be called once at application startup if JWKSURL is configured
func InitJWKSCache(cfg JWTCfg) error {
	if cfg.JWKSURL == "" {
		return nil // No upstream IdP configured, skip initialization
	}

	if globalJWKSCache != nil {
		return nil // Already initialized
	}

	globalJWKSCache = &jwksCache{
		keys:     make(map[string]*rsa.PublicKey),
		cacheTTL: 1 * time.Hour,
		jwksURL:  cfg.JWKSURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second, // Prevent hanging on slow/stalled JWKS endpoint
		},
	}

	// Pre-fetch JWKS on startup
	if err := globalJWKSCache.fetchJWKS(false); err != nil {
		log.Warn().Err(err).Msg("failed to pre-fetch JWKS (will retry on first request)")
		return err
	}

	log.Info().Str("jwks_url", cfg.JWKSURL).Msg("upstream IdP RS256 validation enabled")
	return nil
}

// Middleware creates HTTP middleware for JWT authentication
// Supports three modes:
// 1. Production RS256: Upstream IdP Bearer tokens with RS256 signature validation
// 2. Development HS256: Bearer tokens with HMAC secret (for testing)
// 3. Development X-Debug-Sub: Bypass JWT validation (ONLY when DevMode=true)
func Middleware(db *pgxpool.Pool, cfg JWTCfg) func(http.Handler) http.Handler {
	// Initialize JWKS cache for upstream IdP RS256 validation
	_ = InitJWKSCache(cfg)

	// Log warning if dev mode is enabled
	if cfg.DevMode {
		log.Warn().Msg("SECURITY WARNING: DevMode enabled - X-Debug-Sub header will bypass JWT authentication")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract token from Authorization header
			tok := ""
			if h := r.Header.Get("Authorization"); len(h) > 7 && h[:7] == "Bearer " {
				tok = h[7:]
			}

			sub := ""

			// Development mode: accept X-Debug-Sub ONLY if DevMode is enabled and no token present
			if cfg.DevMode && tok == "" {
				sub = r.Header.Get("X-Debug-Sub")
				if sub != "" {
					log.Debug().Str("sub", sub).Msg("using X-Debug-Sub header (dev mode)")
				}
			}

			// Validate JWT token if present
			var claims jwt.MapClaims
			if tok != "" {
				var err error
				sub, claims, err = ValidateToken(tok, cfg)
				if err != nil {
					log.Warn().Err(err).Msg("jwt validation failed")
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
			}

			// Require subject (either from JWT or debug header)
			if sub == "" {
				log.Warn().Msg("missing subject (no JWT sub or X-Debug-Sub header)")
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			// Upsert app_user by subject (creates user on first auth)
			var userID string
			if err := db.QueryRow(r.Context(),
				`INSERT INTO app_user (sub) VALUES ($1)
				 ON CONFLICT (sub) DO UPDATE SET sub = excluded.sub
				 RETURNING id`, sub).Scan(&userID); err != nil {
				log.Error().Err(err).Str("sub", sub).Msg("failed to upsert user")
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}

			// Add user ID and subject to request context
			ctx := context.WithValue(r.Context(), CtxUserID, userID)
			ctx = context.WithValue(ctx, CtxSubject, sub)

			// Extract tenant from JWT claims if configured and not already set by header middleware
			// Precedence: X-TB-Tenant-ID header (if present) > JWT tenant claim > no tenant
			//
			// TENANT IDENTITY CONTRACT:
			// The claim value (e.g., cfg.TenantClaim = "organization_id") contains the IdP-level
			// organization ID, which is the logical tenant identifier used throughout the system.
			// This is NOT a database-specific identifier (e.g., NOT a Neon branch ID).
			//
			// Future database routing (Neon DB-per-tenant) will use this organization ID to
			// look up the correct connection pool via tenant_registry.
			//
			// See: Plans/neon-migration-tenant-contract.md
			if TenantID(ctx) == "" && cfg.TenantClaim != "" && claims != nil {
				if tenantVal, ok := claims[cfg.TenantClaim]; ok {
					if tenantID, ok := tenantVal.(string); ok && tenantID != "" {
						ctx = context.WithValue(ctx, TenantIDKey, tenantID)
						log.Debug().Str("tenant_id", tenantID).Str("claim", cfg.TenantClaim).Msg("tenant derived from JWT claim")
					}
				} else {
					// Tenant claim not found in JWT - this is expected for backend-driven tenant resolution
					// (via /v1/auth/tenant) or header-based tenancy (X-TB-Tenant-ID)
					// Only log at trace level to avoid confusion
					log.Trace().
						Str("claim", cfg.TenantClaim).
						Msg("tenant claim not found in JWT (expected for backend-driven tenant resolution or header-based tenancy)")
				}
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserID extracts the authenticated user ID (database ID) from request context
// Returns empty string if not authenticated (should never happen after middleware)
func UserID(ctx context.Context) string {
	if v := ctx.Value(CtxUserID); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// Subject extracts the OIDC subject claim (JWT sub, WorkOS user ID) from request context
// Returns empty string if not authenticated (should never happen after middleware)
// Use this for WorkOS API calls, not UserID() which returns the database primary key
func Subject(ctx context.Context) string {
	if v := ctx.Value(CtxSubject); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
