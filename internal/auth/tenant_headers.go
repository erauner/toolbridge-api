package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
)

// Tenant context key for storing validated tenant ID
type tenantCtxKey string

const TenantIDKey tenantCtxKey = "tenant_id"

var (
	ErrMissingTenantID  = errors.New("missing X-TB-Tenant-ID header")
	ErrMissingTimestamp = errors.New("missing X-TB-Timestamp header")
	ErrMissingSignature = errors.New("missing X-TB-Signature header")
	ErrInvalidTimestamp = errors.New("invalid timestamp format")
	ErrTimestampSkew    = errors.New("timestamp outside acceptable window")
	ErrInvalidSignature = errors.New("invalid HMAC signature")
)

// TenantHeaders contains validated tenant context
type TenantHeaders struct {
	TenantID  string
	Timestamp time.Time
}

// ValidateTenantHeaders validates HMAC-signed tenant headers
// Returns validated tenant headers or an error if validation fails
func ValidateTenantHeaders(r *http.Request, secret string, maxSkewSeconds int64) (*TenantHeaders, error) {
	// Extract headers
	tenantID := r.Header.Get("X-TB-Tenant-ID")
	timestampStr := r.Header.Get("X-TB-Timestamp")
	signature := r.Header.Get("X-TB-Signature")

	if tenantID == "" {
		return nil, ErrMissingTenantID
	}
	if timestampStr == "" {
		return nil, ErrMissingTimestamp
	}
	if signature == "" {
		return nil, ErrMissingSignature
	}

	// Parse timestamp
	timestampMs, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTimestamp, err)
	}

	// Validate timestamp within acceptable window
	now := time.Now()
	requestTime := time.UnixMilli(timestampMs)
	skew := now.Sub(requestTime).Abs().Seconds()

	if skew > float64(maxSkewSeconds) {
		log.Warn().
			Str("tenant_id", tenantID).
			Int64("timestamp_ms", timestampMs).
			Float64("skew_seconds", skew).
			Int64("max_skew", maxSkewSeconds).
			Msg("tenant header timestamp outside acceptable window")
		return nil, ErrTimestampSkew
	}

	// Verify HMAC signature
	// Message format: "{tenant_id}:{timestamp_ms}"
	message := fmt.Sprintf("%s:%s", tenantID, timestampStr)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	// Constant-time comparison to prevent timing attacks
	if !hmac.Equal([]byte(expectedSig), []byte(signature)) {
		log.Warn().
			Str("tenant_id", tenantID).
			Str("expected_sig_prefix", expectedSig[:16]+"...").
			Str("actual_sig_prefix", signature[:min(16, len(signature))]+"...").
			Msg("tenant header signature mismatch")
		return nil, ErrInvalidSignature
	}

	log.Debug().
		Str("tenant_id", tenantID).
		Int64("timestamp_ms", timestampMs).
		Float64("skew_seconds", skew).
		Msg("tenant headers validated successfully")

	return &TenantHeaders{
		TenantID:  tenantID,
		Timestamp: requestTime,
	}, nil
}

// TenantHeaderMiddleware validates tenant headers on all requests
// This should be applied AFTER JWT middleware to ensure both user and tenant contexts are available
func TenantHeaderMiddleware(secret string, maxSkewSeconds int64) func(http.Handler) http.Handler {
	if secret == "" {
		log.Fatal().Msg("TENANT_HEADER_SECRET is required for tenant header middleware")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Validate tenant headers
			headers, err := ValidateTenantHeaders(r, secret, maxSkewSeconds)
			if err != nil {
				log.Error().
					Err(err).
					Str("path", r.URL.Path).
					Str("method", r.Method).
					Msg("tenant header validation failed")
				
				http.Error(w, "Unauthorized: invalid tenant headers", http.StatusUnauthorized)
				return
			}

			// Store tenant context in request
			ctx := context.WithValue(r.Context(), TenantIDKey, headers.TenantID)

			log.Debug().
				Str("tenant_id", headers.TenantID).
				Str("path", r.URL.Path).
				Str("method", r.Method).
				Msg("tenant headers validated and context stored")

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// TenantID extracts tenant ID from request context
// Returns empty string if tenant ID not found in context
func TenantID(ctx context.Context) string {
	if tenantID, ok := ctx.Value(TenantIDKey).(string); ok {
		return tenantID
	}
	return ""
}

// min helper function for Go < 1.21 compatibility
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
