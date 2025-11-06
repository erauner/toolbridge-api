package httpapi

import (
	"context"
	"net/http"

	"github.com/rs/zerolog/log"
)

type contextKey string

const (
	sessionIDKey contextKey = "sessionId"
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
