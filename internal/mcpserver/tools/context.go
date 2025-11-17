package tools

import (
	"errors"

	"github.com/erauner12/toolbridge-api/internal/mcpserver/client"
	"github.com/rs/zerolog"
)

// Attachment-related errors (client errors - invalid parameters)
var (
	ErrAttachmentLimitExceeded = errors.New("attachment limit exceeded")
	ErrAttachmentAlreadyExists = errors.New("entity already attached")
	ErrAttachmentNotFound      = errors.New("attachment not found")
	ErrSessionNotFound         = errors.New("session not found or expired")
)

// SessionManager interface for attachment management (to avoid circular imports)
type SessionManager interface {
	AddAttachment(sessionID string, att Attachment) error
	RemoveAttachment(sessionID, entityUID, entityKind string) error
	ListAttachments(sessionID string) ([]Attachment, error)
	ClearAttachments(sessionID string) error
}

// Attachment represents a context item (matches server.Attachment)
type Attachment struct {
	UID   string `json:"uid"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
}

// ToolContext provides shared resources for tool handlers
type ToolContext struct {
	Logger         *zerolog.Logger
	UserID         string
	SessionID      string
	RESTClients    map[string]*client.EntityClient
	SessionManager SessionManager // For context attachment management
}

// NewToolContext creates a context with entity clients for all supported types
func NewToolContext(logger *zerolog.Logger, userID, sessionID string, httpClient *client.HTTPClient, sessionMgr SessionManager) *ToolContext {
	return &ToolContext{
		Logger:         logger,
		UserID:         userID,
		SessionID:      sessionID,
		SessionManager: sessionMgr,
		RESTClients: map[string]*client.EntityClient{
			"notes":         client.NewEntityClient(httpClient, "notes"),
			"tasks":         client.NewEntityClient(httpClient, "tasks"),
			"comments":      client.NewEntityClient(httpClient, "comments"),
			"chats":         client.NewEntityClient(httpClient, "chats"),
			"chat_messages": client.NewEntityClient(httpClient, "chat_messages"),
		},
	}
}

// GetClient retrieves an entity client by type
func (tc *ToolContext) GetClient(entityType string) (*client.EntityClient, error) {
	c, ok := tc.RESTClients[entityType]
	if !ok {
		return nil, NewToolError(ErrCodeInvalidParams, "Unknown entity type: "+entityType, nil)
	}
	return c, nil
}
