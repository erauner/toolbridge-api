package client

import (
	"context"
	"time"

	"github.com/erauner12/toolbridge-api/internal/mcpserver/auth"
)

// TokenProvider abstracts the Auth0 token broker from Phase 2
// This allows for easy mocking in tests and dev mode bypass
type TokenProvider interface {
	// GetToken acquires an access token for the given audience
	GetToken(ctx context.Context, audience, scope string, interactive bool) (*auth.TokenResult, error)

	// InvalidateToken removes a token from cache (e.g., on 401)
	InvalidateToken(audience, scope string)
}

// SessionProvider abstracts the session manager for testing
type SessionProvider interface {
	// EnsureSession returns a valid session, creating or refreshing as needed
	EnsureSession(ctx context.Context) (*Session, error)

	// InvalidateSession clears the cached session
	InvalidateSession()
}

// Session represents a REST API sync session
// Reference: internal/httpapi/sessions.go
type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Epoch     int       `json:"epoch"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// RESTItem represents a single entity from the REST API
// Reference: internal/service/syncservice/rest_types.go
type RESTItem struct {
	UID       string         `json:"uid"`
	Version   int            `json:"version"`
	UpdatedAt string         `json:"updatedAt"`
	DeletedAt *string        `json:"deletedAt,omitempty"`
	Payload   map[string]any `json:"payload"`
}

// RESTListResponse represents a paginated list response
type RESTListResponse struct {
	Items      []RESTItem `json:"items"`
	NextCursor *string    `json:"nextCursor,omitempty"`
}

// ListOpts configures list operations
type ListOpts struct {
	Cursor         string
	Limit          int
	IncludeDeleted bool
}
