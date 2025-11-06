package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

type contextKey string

const (
	sessionIDKey     contextKey = "sessionId"
	correlationIDKey contextKey = "correlationId"
)

// SessionMiddleware reads X-Sync-Session header and adds it to context
// This allows correlation of all sync operations within a session
func SessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.Header.Get("X-Sync-Session")
		
		if sessionID != "" {
			// Add to context for downstream handlers
			ctx := context.WithValue(r.Context(), sessionIDKey, sessionID)
			r = r.WithContext(ctx)

			// Add to logger context for all logs in this request
			logger := log.With().Str("sessionId", sessionID).Logger()
			r = r.WithContext(logger.WithContext(r.Context()))
		}

		next.ServeHTTP(w, r)
	})
}

// GetSessionID retrieves the session ID from context
func GetSessionID(ctx context.Context) string {
	if sessionID, ok := ctx.Value(sessionIDKey).(string); ok {
		return sessionID
	}
	return ""
}

// CorrelationMiddleware reads X-Correlation-ID header and adds it to context
// Generates a new correlation ID if client doesn't provide one
// This enables end-to-end request tracing across client and server logs
func CorrelationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract correlation ID from request header
		correlationID := r.Header.Get("X-Correlation-ID")
		if correlationID == "" {
			// Generate one if client didn't provide it
			correlationID = uuid.New().String()
		}

		// Add to response headers for client verification
		w.Header().Set("X-Correlation-ID", correlationID)

		// Store in context for downstream handlers
		ctx := context.WithValue(r.Context(), correlationIDKey, correlationID)

		// Add to logger context for all logs in this request
		logger := log.With().Str("correlation_id", correlationID).Logger()
		ctx = logger.WithContext(ctx)

		r = r.WithContext(ctx)
		next.ServeHTTP(w, r)
	})
}

// GetCorrelationID retrieves the correlation ID from context
func GetCorrelationID(ctx context.Context) string {
	if correlationID, ok := ctx.Value(correlationIDKey).(string); ok {
		return correlationID
	}
	return ""
}
