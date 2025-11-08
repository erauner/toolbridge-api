package httpapi

import (
	"net/http"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/erauner12/toolbridge-api/internal/session"
	"github.com/rs/zerolog/log"
)

// Use the global shared session store
var sessionStore = session.GetStore()

// SessionRequired middleware enforces that a valid sync session is active
// This should be applied to all sync entity endpoints (push/pull)
// but NOT to /info or session management endpoints
func SessionRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get session ID from context (set by SessionMiddleware)
		sessionID := GetSessionID(r.Context())

		if sessionID == "" {
			log.Warn().
				Str("path", r.URL.Path).
				Str("method", r.Method).
				Msg("Request to sync endpoint without X-Sync-Session header")

			writeError(w, r, http.StatusPreconditionRequired,
				"X-Sync-Session header required. Please call POST /v1/sync/sessions to begin a session.")
			return
		}

		// Validate that the session exists and is not expired
		session, ok := sessionStore.GetSession(sessionID)
		if !ok {
			log.Warn().
				Str("sessionId", sessionID).
				Str("path", r.URL.Path).
				Msg("Invalid or expired sync session")

			writeError(w, r, http.StatusPreconditionRequired,
				"Invalid or expired sync session. Please call POST /v1/sync/sessions to begin a new session.")
			return
		}

		// Validate that the session belongs to the authenticated user
		authenticatedUserID := auth.UserID(r.Context())
		if session.UserID != authenticatedUserID {
			log.Warn().
				Str("sessionId", sessionID).
				Str("sessionUserId", session.UserID).
				Str("authenticatedUserId", authenticatedUserID).
				Str("path", r.URL.Path).
				Msg("Session does not belong to authenticated user")

			writeError(w, r, http.StatusForbidden,
				"Session does not belong to authenticated user.")
			return
		}

		// Session is valid and belongs to the authenticated user, proceed with request
		next.ServeHTTP(w, r)
	})
}
