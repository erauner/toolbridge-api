package httpapi

import (
	"net/http"
	"sync"
	"time"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Session represents an active sync session
type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// SessionStore manages active sync sessions
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]Session // key: sessionId
	ttl      time.Duration
}

// Global session store (in-memory for now)
var sessionStore = &SessionStore{
	sessions: make(map[string]Session),
	ttl:      30 * time.Minute, // Sessions expire after 30 minutes
}

// CreateSession generates a new session ID for the user
func (s *SessionStore) CreateSession(userID string) Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	session := Session{
		ID:        uuid.New().String(),
		UserID:    userID,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(s.ttl),
	}

	s.sessions[session.ID] = session

	// Clean up expired sessions opportunistically
	s.cleanupExpiredLocked()

	return session
}

// GetSession retrieves a session by ID
func (s *SessionStore) GetSession(sessionID string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return Session{}, false
	}

	// Check if expired
	if time.Now().UTC().After(session.ExpiresAt) {
		return Session{}, false
	}

	return session, true
}

// DeleteSession removes a session
func (s *SessionStore) DeleteSession(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.sessions[sessionID]
	if exists {
		delete(s.sessions, sessionID)
	}

	return exists
}

// cleanupExpiredLocked removes expired sessions (caller must hold write lock)
func (s *SessionStore) cleanupExpiredLocked() {
	now := time.Now().UTC()
	for id, session := range s.sessions {
		if now.After(session.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
}

// HTTP Handlers

// BeginSession handles POST /v1/sync/sessions
// Creates a new sync session for the authenticated user
func (s *Server) BeginSession(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	session := sessionStore.CreateSession(userID)

	log.Info().
		Str("sessionId", session.ID).
		Str("userId", userID).
		Time("expiresAt", session.ExpiresAt).
		Msg("sync session created")

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
