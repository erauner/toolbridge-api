package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/workos/workos-go/v6/pkg/usermanagement"
)

// Tenant context key for storing validated tenant ID
//
// TENANT IDENTITY CONTRACT:
// The value stored at this key is the logical tenant identifier (IdP organization ID),
// NOT a database-specific artifact. This enables future migration to Neon DB-per-tenant
// without changing how tenant identity is resolved and propagated through the request context.
//
// See: Plans/neon-migration-tenant-contract.md
type tenantCtxKey string

const TenantIDKey tenantCtxKey = "tenant_id"

var (
	ErrMissingTenantID    = errors.New("missing X-TB-Tenant-ID header")
	ErrUnauthorizedTenant = errors.New("user not authorized for tenant")
)

// TenantAuthCache is a simple in-memory cache for tenant authorization validation
// Cache key format: "subject:tenant_id" -> expiry time
// TTL: 5 minutes (balances security vs. performance)
// NOTE: subject is the OIDC subject claim (JWT sub), not the database user ID
type TenantAuthCache struct {
	mu    sync.RWMutex
	cache map[string]time.Time
}

// NewTenantAuthCache creates a new tenant authorization cache
func NewTenantAuthCache() *TenantAuthCache {
	cache := &TenantAuthCache{
		cache: make(map[string]time.Time),
	}

	// Start background cleanup goroutine to prevent memory leaks
	go cache.cleanupExpired()

	return cache
}

// Get checks if a subject+tenant combination is cached and not expired
func (c *TenantAuthCache) Get(subject, tenantID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", subject, tenantID)
	expiry, exists := c.cache[key]

	if !exists {
		return false
	}

	// Check if expired
	if time.Now().After(expiry) {
		return false
	}

	return true
}

// Set caches a subject+tenant combination with 5-minute TTL
func (c *TenantAuthCache) Set(subject, tenantID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := fmt.Sprintf("%s:%s", subject, tenantID)
	c.cache[key] = time.Now().Add(5 * time.Minute)
}

// cleanupExpired removes expired cache entries every minute
func (c *TenantAuthCache) cleanupExpired() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for key, expiry := range c.cache {
			if now.After(expiry) {
				delete(c.cache, key)
			}
		}
		c.mu.Unlock()
	}
}

// validateTenantAuthorization validates that a user is authorized to access a specific tenant.
// Uses WorkOS API to verify organization membership with in-memory caching.
//
// IMPORTANT: subject must be the OIDC subject claim (JWT sub), not the database user ID.
// WorkOS API requires the IdP user ID, which is the JWT sub claim.
//
// Returns true if authorized, false otherwise.
func validateTenantAuthorization(ctx context.Context, subject, tenantID string, client *usermanagement.Client, cache *TenantAuthCache, defaultTenantID string) bool {
	// Check cache first
	if cache.Get(subject, tenantID) {
		log.Debug().
			Str("subject", subject).
			Str("tenant_id", tenantID).
			Msg("tenant authorization cached (valid)")
		return true
	}

	// Single-tenant/smoke-test mode: WorkOS client not configured
	// SECURITY FIX: Fail-closed - only allow access to the default tenant when WorkOS is not configured.
	// This prevents arbitrary tenant access when WORKOS_API_KEY is missing/misconfigured.
	// Production multi-tenant deployments MUST set WORKOS_API_KEY for proper B2B validation.
	if client == nil {
		// Only allow access to the default tenant (B2C mode)
		if defaultTenantID != "" && tenantID == defaultTenantID {
			log.Warn().
				Str("subject", subject).
				Str("tenant_id", tenantID).
				Msg("WorkOS client not configured - allowing default tenant access only (single-tenant/B2C mode)")
			cache.Set(subject, tenantID)
			return true
		}
		// Reject all other tenant access attempts when WorkOS is not configured
		log.Error().
			Str("subject", subject).
			Str("tenant_id", tenantID).
			Str("default_tenant_id", defaultTenantID).
			Msg("WorkOS client not configured and requested tenant does not match default - denying access (fail-closed)")
		return false
	}

	// B2B validation: Call WorkOS API to verify organization membership
	// Paginate through all memberships to handle users with many orgs
	var allMemberships []usermanagement.OrganizationMembership
	var cursor string

	for {
		opts := usermanagement.ListOrganizationMembershipsOpts{
			UserID: subject, // WorkOS API expects OIDC sub (IdP user ID), not database ID
			Limit:  100,     // Fetch in larger batches for efficiency
		}
		if cursor != "" {
			opts.After = cursor
		}

		memberships, err := client.ListOrganizationMemberships(ctx, opts)
		if err != nil {
			log.Error().
				Err(err).
				Str("subject", subject).
				Str("tenant_id", tenantID).
				Msg("Failed to validate tenant authorization via WorkOS API")
			return false
		}

		// Check if user is member of requested organization (B2B path)
		// Check as we paginate for early exit on match
		for _, membership := range memberships.Data {
			if membership.OrganizationID == tenantID {
				log.Info().
					Str("subject", subject).
					Str("tenant_id", tenantID).
					Str("organization_name", membership.OrganizationName).
					Msg("Tenant authorization validated via WorkOS API")
				cache.Set(subject, tenantID)
				return true
			}
		}

		allMemberships = append(allMemberships, memberships.Data...)

		// Check if there are more pages (After is empty when no more pages)
		if memberships.ListMetadata.After == "" {
			break
		}
		cursor = memberships.ListMetadata.After
	}

	// B2C fallback: Allow default tenant ONLY if user has NO organization memberships
	// This prevents B2B users from spoofing the default tenant header to bypass org validation
	if tenantID == defaultTenantID && len(allMemberships) == 0 {
		log.Debug().
			Str("subject", subject).
			Str("tenant_id", tenantID).
			Msg("B2C user accessing default tenant (no organization memberships)")
		cache.Set(subject, tenantID)
		return true
	}

	// Rejection: User either requested wrong org, or requested default tenant while having org memberships
	if tenantID == defaultTenantID && len(allMemberships) > 0 {
		log.Warn().
			Str("subject", subject).
			Str("tenant_id", tenantID).
			Int("membership_count", len(allMemberships)).
			Msg("B2B user attempted to access default tenant - must use organization tenant")
	} else {
		log.Warn().
			Str("subject", subject).
			Str("tenant_id", tenantID).
			Int("membership_count", len(allMemberships)).
			Msg("User not authorized for requested tenant (no matching organization membership)")
	}
	return false
}

// SimpleTenantHeaderMiddleware validates the X-TB-Tenant-ID header with WorkOS authorization check.
// This is the recommended middleware for multi-tenant MCP deployments where the MCP server
// handles authentication via OAuth and sends a plain tenant ID header.
//
// SECURITY: Validates that the authenticated user is actually authorized to access the requested tenant
// by checking organization membership via WorkOS API (with caching).
//
// This should be applied AFTER JWT middleware to ensure user authentication is already validated.
func SimpleTenantHeaderMiddleware(workosClient *usermanagement.Client, cache *TenantAuthCache, defaultTenantID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Extract tenant ID from header
			tenantID := r.Header.Get("X-TB-Tenant-ID")
			if tenantID == "" {
				log.Error().
					Str("path", r.URL.Path).
					Str("method", r.Method).
					Msg("tenant header validation failed: missing header")

				http.Error(w, "Unauthorized: missing tenant header", http.StatusUnauthorized)
				return
			}

			// Extract OIDC subject from JWT context (set by JWT middleware)
			// NOTE: Use Subject() not UserID() - WorkOS API requires OIDC sub, not database ID
			subject := Subject(ctx)
			if subject == "" {
				log.Error().
					Str("path", r.URL.Path).
					Str("method", r.Method).
					Str("tenant_id", tenantID).
					Msg("tenant header validation failed: missing subject from JWT context")

				http.Error(w, "Unauthorized: invalid authentication", http.StatusUnauthorized)
				return
			}

			// Validate tenant authorization via WorkOS API (with caching)
			if !validateTenantAuthorization(ctx, subject, tenantID, workosClient, cache, defaultTenantID) {
				log.Warn().
					Str("subject", subject).
					Str("tenant_id", tenantID).
					Str("path", r.URL.Path).
					Str("method", r.Method).
					Msg("tenant header validation failed: user not authorized for tenant")

				http.Error(w, "Unauthorized: not authorized for requested tenant", http.StatusForbidden)
				return
			}

			// Store tenant context in request
			ctx = context.WithValue(ctx, TenantIDKey, tenantID)

			log.Debug().
				Str("subject", subject).
				Str("tenant_id", tenantID).
				Str("path", r.URL.Path).
				Str("method", r.Method).
				Msg("tenant header validated with authorization check")

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// TenantID extracts the logical tenant identifier from request context.
//
// IMPORTANT - Tenant Identity Contract:
// This returns the IdP-level organization ID (e.g., WorkOS organization_id),
// NOT a database artifact like a Neon branch ID, schema name, or connection string.
//
// The tenant ID should be:
//   - Stable across database migrations (row-level → Neon DB-per-tenant)
//   - Derived from OIDC claims (configurable via TENANT_CLAIM env var)
//   - Used as the key in tenant_registry for future Neon routing
//
// DO NOT change this to return database-specific identifiers.
// Future database routing (e.g., PoolManager.GetTenantPool) should use this
// value to look up the correct connection pool, treating it as the source of truth.
//
// See: Plans/neon-migration-tenant-contract.md
//
// Returns empty string if tenant ID not found in context
func TenantID(ctx context.Context) string {
	if tenantID, ok := ctx.Value(TenantIDKey).(string); ok {
		return tenantID
	}
	return ""
}

