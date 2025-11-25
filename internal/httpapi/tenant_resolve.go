package httpapi

import (
	"net/http"
	"strings"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/rs/zerolog/log"
	"github.com/workos/workos-go/v6/pkg/usermanagement"
)

// TenantResolveResponse is the response for GET /v1/auth/tenant
type TenantResolveResponse struct {
	TenantID         string   `json:"tenant_id"`
	OrganizationName string   `json:"organization_name,omitempty"`
	Organizations    []OrgInfo `json:"organizations,omitempty"` // Multiple organizations case
	RequiresSelection bool    `json:"requires_selection"`
}

// OrgInfo represents organization information for multi-org users
type OrgInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ResolveTenant resolves the tenant ID for the authenticated user by calling WorkOS API
//
// GET /v1/auth/tenant
// Headers: Authorization: Bearer <id_token>
//
// Process:
// 1. Validates ID token (via auth middleware)
// 2. Extracts user ID (sub) from token claims
// 3. Calls WorkOS ListOrganizationMemberships API with server-side API key
// 4. Returns organization ID(s) for the user
//
// Handles multiple scenarios:
// - No organizations (B2C): Returns default tenant "tenant_thinkpen_b2c"
// - Single organization (B2B): Returns tenant_id directly
// - Multiple organizations (B2B): Returns all organizations and sets requires_selection=true
func (s *Server) ResolveTenant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract and validate JWT token
	authHeader := r.Header.Get("Authorization")
	if len(authHeader) < 8 || authHeader[:7] != "Bearer " {
		writeError(w, r, http.StatusUnauthorized, "Missing or invalid Authorization header")
		return
	}

	token := authHeader[7:]
	sub, _, err := auth.ValidateToken(token, s.JWTCfg)
	if err != nil {
		log.Error().
			Err(err).
			Str("correlation_id", GetCorrelationID(ctx)).
			Msg("Token validation failed")
		writeError(w, r, http.StatusUnauthorized, "Invalid token")
		return
	}

	if sub == "" {
		writeError(w, r, http.StatusUnauthorized, "Missing sub claim")
		return
	}

	// Check if WorkOS is configured
	if s.WorkOSClient == nil {
		writeError(w, r, http.StatusServiceUnavailable, "WorkOS tenant resolution not configured")
		return
	}

	// Call WorkOS API to get user's organization memberships
	// Paginate through all memberships to handle users with many orgs
	log.Info().
		Str("user_id", sub).
		Str("correlation_id", GetCorrelationID(ctx)).
		Msg("Resolving tenant via WorkOS API")

	var allMemberships []usermanagement.OrganizationMembership
	var cursor string

	for {
		opts := usermanagement.ListOrganizationMembershipsOpts{
			UserID: sub,
			Limit:  100, // Fetch in larger batches for efficiency
		}
		if cursor != "" {
			opts.After = cursor
		}

		memberships, err := s.WorkOSClient.ListOrganizationMemberships(ctx, opts)
		if err != nil {
			log.Error().
				Err(err).
				Str("user_id", sub).
				Str("correlation_id", GetCorrelationID(ctx)).
				Msg("Failed to resolve tenant from WorkOS")
			writeError(w, r, http.StatusInternalServerError, "Failed to resolve tenant")
			return
		}

		allMemberships = append(allMemberships, memberships.Data...)

		// Check if there are more pages (After is empty when no more pages)
		if memberships.ListMetadata.After == "" {
			break
		}
		cursor = memberships.ListMetadata.After
	}

	// B2C fallback: if user has no organization memberships, use default tenant
	// Pattern 3 (Hybrid): B2C users without org memberships get the default tenant,
	// B2B users with org memberships get their organization ID as tenant
	if len(allMemberships) == 0 {
		log.Info().
			Str("user_id", sub).
			Str("tenant_id", s.DefaultTenantID).
			Str("correlation_id", GetCorrelationID(ctx)).
			Msg("B2C user: assigned default tenant (no organization memberships)")

		writeJSON(w, http.StatusOK, TenantResolveResponse{
			TenantID:          s.DefaultTenantID,
			OrganizationName:  "ThinkPen",
			RequiresSelection: false,
		})
		return
	}

	// Single organization - return directly
	if len(allMemberships) == 1 {
		org := allMemberships[0]
		log.Info().
			Str("user_id", sub).
			Str("tenant_id", org.OrganizationID).
			Str("organization_name", org.OrganizationName).
			Str("correlation_id", GetCorrelationID(ctx)).
			Msg("Tenant resolved successfully")

		writeJSON(w, http.StatusOK, TenantResolveResponse{
			TenantID:          org.OrganizationID,
			OrganizationName:  org.OrganizationName,
			RequiresSelection: false,
		})
		return
	}

	// Multiple organizations - return all and let client choose
	organizations := make([]OrgInfo, len(allMemberships))
	orgNames := make([]string, len(allMemberships))
	for i, membership := range allMemberships {
		organizations[i] = OrgInfo{
			ID:   membership.OrganizationID,
			Name: membership.OrganizationName,
		}
		orgNames[i] = membership.OrganizationName
	}

	log.Info().
		Str("user_id", sub).
		Int("organization_count", len(allMemberships)).
		Str("organizations", strings.Join(orgNames, ", ")).
		Str("correlation_id", GetCorrelationID(ctx)).
		Msg("User belongs to multiple organizations")

	writeJSON(w, http.StatusOK, TenantResolveResponse{
		Organizations:    organizations,
		RequiresSelection: true,
	})
}
