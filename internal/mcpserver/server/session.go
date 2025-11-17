package server

import (
	"fmt"
	"sync"
	"time"

	"github.com/erauner12/toolbridge-api/internal/mcpserver/tools"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// MCPSession represents an active MCP client connection
type MCPSession struct {
	ID          string
	UserID      string // From JWT sub claim
	CreatedAt   time.Time
	LastSeen    time.Time
	Attachments []tools.Attachment // In-memory context attachments
	// SSE connection tracking would go here in future
}

// SessionManager manages MCP sessions
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*MCPSession // sessionID -> session
	ttl      time.Duration
}

// NewSessionManager creates a new session manager
func NewSessionManager(ttl time.Duration) *SessionManager {
	mgr := &SessionManager{
		sessions: make(map[string]*MCPSession),
		ttl:      ttl,
	}

	// Start cleanup goroutine
	go mgr.cleanupExpired()

	return mgr
}

// CreateSession creates a new MCP session for a user
func (sm *SessionManager) CreateSession(userID string) *MCPSession {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session := &MCPSession{
		ID:          uuid.New().String(),
		UserID:      userID,
		CreatedAt:   time.Now(),
		LastSeen:    time.Now(),
		Attachments: make([]tools.Attachment, 0),
	}

	sm.sessions[session.ID] = session

	log.Debug().
		Str("sessionId", session.ID).
		Str("userId", userID).
		Msg("Created MCP session")

	return session
}

// GetSession retrieves a session by ID
func (sm *SessionManager) GetSession(sessionID string) (*MCPSession, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session not found")
	}

	return session, nil
}

// UpdateLastSeen updates the last seen time for a session
func (sm *SessionManager) UpdateLastSeen(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if session, exists := sm.sessions[sessionID]; exists {
		session.LastSeen = time.Now()
	}
}

// DeleteSession removes a session
func (sm *SessionManager) DeleteSession(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.sessions, sessionID)

	log.Debug().
		Str("sessionId", sessionID).
		Msg("Deleted MCP session")
}

// AddAttachment adds a context attachment to a session
// Returns error if session not found or attachment limit exceeded
func (sm *SessionManager) AddAttachment(sessionID string, att tools.Attachment) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found")
	}

	// Enforce attachment limit (prevent memory growth)
	const maxAttachments = 50
	if len(session.Attachments) >= maxAttachments {
		return fmt.Errorf("attachment limit exceeded (max %d)", maxAttachments)
	}

	// Check for duplicates (by both UID and kind, as different entity types may share UUIDs)
	for _, existing := range session.Attachments {
		if existing.UID == att.UID && existing.Kind == att.Kind {
			return fmt.Errorf("entity already attached")
		}
	}

	session.Attachments = append(session.Attachments, att)

	log.Debug().
		Str("sessionId", sessionID).
		Str("entityUID", att.UID).
		Str("entityKind", att.Kind).
		Msg("Added context attachment to MCP session")

	return nil
}

// RemoveAttachment removes a context attachment from a session
func (sm *SessionManager) RemoveAttachment(sessionID, entityUID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found")
	}

	// Find and remove the attachment
	found := false
	filtered := make([]tools.Attachment, 0, len(session.Attachments))
	for _, att := range session.Attachments {
		if att.UID != entityUID {
			filtered = append(filtered, att)
		} else {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("attachment not found")
	}

	session.Attachments = filtered

	log.Debug().
		Str("sessionId", sessionID).
		Str("entityUID", entityUID).
		Msg("Removed context attachment from MCP session")

	return nil
}

// ListAttachments returns all attachments for a session
func (sm *SessionManager) ListAttachments(sessionID string) ([]tools.Attachment, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session not found")
	}

	// Return a copy to prevent external modification
	attachments := make([]tools.Attachment, len(session.Attachments))
	copy(attachments, session.Attachments)

	return attachments, nil
}

// ClearAttachments removes all attachments from a session
func (sm *SessionManager) ClearAttachments(sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found")
	}

	session.Attachments = make([]tools.Attachment, 0)

	log.Debug().
		Str("sessionId", sessionID).
		Msg("Cleared all context attachments from MCP session")

	return nil
}

// cleanupExpired removes expired sessions
func (sm *SessionManager) cleanupExpired() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		sm.mu.Lock()
		now := time.Now()
		expired := 0
		for id, session := range sm.sessions {
			if now.Sub(session.LastSeen) > sm.ttl {
				delete(sm.sessions, id)
				expired++
			}
		}
		sm.mu.Unlock()

		if expired > 0 {
			log.Info().
				Int("count", expired).
				Msg("Cleaned up expired MCP sessions")
		}
	}
}
