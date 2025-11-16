package auth

import (
	"context"
	"time"

	"github.com/erauner12/toolbridge-api/internal/mcpserver/config"
)

// Delegate handles Auth0 authentication flows
type Delegate interface {
	// Configure initializes the delegate with Auth0 configuration
	Configure(config config.Auth0Config) error

	// EnsureSession establishes or validates an Auth0 session
	// interactive: whether to prompt user for interaction (device code display, etc.)
	// defaultScopes: base scopes to request
	EnsureSession(ctx context.Context, interactive bool, defaultScopes []string) (bool, error)

	// TryGetToken attempts to acquire an access token
	// Returns nil if token cannot be acquired (e.g., no session, user not authenticated)
	TryGetToken(ctx context.Context, audience string, scopes []string, interactive bool) (*TokenResult, error)

	// TryGetIDToken attempts to get the ID token for user info
	TryGetIDToken(ctx context.Context, defaultScopes []string) (string, error)

	// LogoutAll clears all sessions and tokens
	LogoutAll(ctx context.Context) error
}

// TokenResult represents an OAuth2 access token
type TokenResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	TokenType    string // Usually "Bearer"
}
