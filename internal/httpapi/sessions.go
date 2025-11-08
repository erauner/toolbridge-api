package httpapi

import (
	"net/http"
	"strconv"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/erauner12/toolbridge-api/internal/session"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"
)

// Use the global shared session store
var sessionStore = session.GetStore()

// HTTP Handlers

// BeginSession handles POST /v1/sync/sessions
// Creates a new sync session for the authenticated user
func (s *Server) BeginSession(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Load or create owner_state row (lazy initialization)
	var epoch int
	err := s.DB.QueryRow(r.Context(), `
		INSERT INTO owner_state(owner_id, epoch, created_at, updated_at)
		VALUES ($1, 1, NOW(), NOW())
		ON CONFLICT (owner_id) DO NOTHING
		RETURNING epoch
	`, userID).Scan(&epoch)

	if err != nil {
		// If insert did nothing (row exists), select epoch
		if err == pgx.ErrNoRows {
			err = s.DB.QueryRow(r.Context(),
				`SELECT epoch FROM owner_state WHERE owner_id = $1`,
				userID,
			).Scan(&epoch)
			if err != nil {
				log.Error().Err(err).Str("userId", userID).Msg("Failed to load epoch")
				writeError(w, r, http.StatusInternalServerError, "Failed to load epoch")
				return
			}
		} else {
			log.Error().Err(err).Str("userId", userID).Msg("Failed to initialize epoch")
			writeError(w, r, http.StatusInternalServerError, "Failed to initialize epoch")
			return
		}
	}

	// Create session with epoch
	session := sessionStore.CreateSession(userID, epoch)

	log.Info().
		Str("sessionId", session.ID).
		Str("userId", userID).
		Int("epoch", epoch).
		Time("expiresAt", session.ExpiresAt).
		Msg("sync session created")

	// Return session with epoch in header
	w.Header().Set("X-Sync-Epoch", strconv.Itoa(epoch))
	writeJSON(w, http.StatusCreated, session)
}

// EndSession handles DELETE /v1/sync/sessions/{id}
// Ends an active sync session
func (s *Server) EndSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		http.Error(w, "session ID required", http.StatusBadRequest)
		return
	}

	userID := auth.UserID(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Verify session belongs to user
	session, exists := sessionStore.GetSession(sessionID)
	if !exists {
		http.Error(w, "session not found or expired", http.StatusNotFound)
		return
	}

	if session.UserID != userID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	sessionStore.DeleteSession(sessionID)

	log.Info().
		Str("sessionId", sessionID).
		Str("userId", userID).
		Msg("sync session ended")

	w.WriteHeader(http.StatusNoContent)
}

// GetSession handles GET /v1/sync/sessions/{id}
// Retrieves session information (for debugging)
func (s *Server) GetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		http.Error(w, "session ID required", http.StatusBadRequest)
		return
	}

	userID := auth.UserID(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	session, exists := sessionStore.GetSession(sessionID)
	if !exists {
		http.Error(w, "session not found or expired", http.StatusNotFound)
		return
	}

	// Users can only view their own sessions
	if session.UserID != userID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	writeJSON(w, http.StatusOK, session)
}
