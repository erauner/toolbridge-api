package session

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// Session represents an active sync session
type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Epoch     int       `json:"epoch"` // Tenant epoch for wipe/reset coordination
}

// Store manages active sync sessions
type Store struct {
	mu       sync.RWMutex
	sessions map[string]Session // key: sessionId
	ttl      time.Duration
}

// Global session store (in-memory)
var globalStore = &Store{
	sessions: make(map[string]Session),
	ttl:      30 * time.Minute, // Sessions expire after 30 minutes
}

// GetStore returns the singleton session store
func GetStore() *Store {
	return globalStore
}

// CreateSession generates a new session ID for the user
func (s *Store) CreateSession(userID string, epoch int) Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	session := Session{
		ID:        uuid.New().String(),
		UserID:    userID,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(s.ttl),
		Epoch:     epoch,
	}

	s.sessions[session.ID] = session

	// Clean up expired sessions opportunistically
	s.cleanupExpiredLocked()

	return session
}

// GetSession retrieves a session by ID
func (s *Store) GetSession(sessionID string) (Session, bool) {
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
func (s *Store) DeleteSession(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.sessions[sessionID]
	if exists {
		delete(s.sessions, sessionID)
	}

	return exists
}

// DeleteUserSessions removes all sessions for a given user.
// Returns the number of sessions deleted.
// Used when wiping account data to invalidate all device sessions.
func (s *Store) DeleteUserSessions(userID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for id, sess := range s.sessions {
		if sess.UserID == userID {
			delete(s.sessions, id)
			count++
		}
	}
	return count
}

// cleanupExpiredLocked removes expired sessions (caller must hold write lock)
func (s *Store) cleanupExpiredLocked() {
	now := time.Now().UTC()
	for id, session := range s.sessions {
		if now.After(session.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
}
