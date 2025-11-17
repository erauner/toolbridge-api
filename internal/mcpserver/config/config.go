package config

import "time"

// Config holds all configuration for the MCP bridge server
type Config struct {
	Auth0          Auth0Config     `json:"auth0"`
	APIBaseURL     string          `json:"apiBaseUrl"`
	PublicURL      string          `json:"publicUrl,omitempty"` // Public-facing MCP endpoint URL for OAuth metadata
	Workspace      WorkspaceConfig `json:"workspace"`
	Allowlist      []string        `json:"allowlist"`       // deprecated, unused
	AllowedOrigins []string        `json:"allowedOrigins"`  // CORS origin validation (DNS rebinding protection)
	Debug          bool            `json:"debug"`
	DevMode        bool            `json:"devMode"` // enables X-Debug-Sub header fallback
	LogLevel       string          `json:"logLevel"`
}

// Auth0Config mirrors the Dart Auth0Config structure
type Auth0Config struct {
	Domain  string                  `json:"domain"`
	Clients map[string]ClientConfig `json:"clients"` // web, native, macos
	SyncAPI *SyncAPIConfig          `json:"syncApi,omitempty"`
}

// ClientConfig describes a single Auth0 client configuration
type ClientConfig struct {
	ClientID             string            `json:"clientId"`
	RedirectURI          string            `json:"redirectUri,omitempty"`
	RedirectURIOverrides map[string]string `json:"redirectUriOverrides,omitempty"`
	DefaultScopes        []string          `json:"scopes,omitempty"`
	AdditionalParams     map[string]string `json:"additionalParameters,omitempty"`
}

// SyncAPIConfig holds sync API authentication configuration
type SyncAPIConfig struct {
	Audience string `json:"audience"`
	Scope    string `json:"scope,omitempty"`
}

// WorkspaceConfig defines workspace settings for the MCP server
type WorkspaceConfig struct {
	Roots []string `json:"roots,omitempty"`
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if !c.DevMode {
		if err := c.Auth0.Validate(); err != nil {
			return err
		}
	}

	if c.APIBaseURL == "" {
		return ErrMissingAPIBaseURL
	}

	return nil
}

// Validate checks if Auth0 configuration is valid
func (a *Auth0Config) Validate() error {
	if a.Domain == "" {
		return ErrMissingAuth0Domain
	}

	if len(a.Clients) == 0 {
		return ErrMissingAuth0Clients
	}

	// Validate at least one client has a clientId
	hasValidClient := false
	for _, client := range a.Clients {
		if client.ClientID != "" {
			hasValidClient = true
			break
		}
	}

	if !hasValidClient {
		return ErrNoValidAuth0Client
	}

	// Validate SyncAPI configuration is present
	if a.SyncAPI == nil || a.SyncAPI.Audience == "" {
		return ErrMissingSyncAPI
	}

	return nil
}

// GetWebClient returns the web client configuration
func (a *Auth0Config) GetWebClient() *ClientConfig {
	if client, ok := a.Clients["web"]; ok {
		return &client
	}
	return nil
}

// GetNativeClient returns the native client configuration
func (a *Auth0Config) GetNativeClient() *ClientConfig {
	if client, ok := a.Clients["native"]; ok {
		return &client
	}
	return nil
}

// GetMacOSClient returns the macOS client configuration
func (a *Auth0Config) GetMacOSClient() *ClientConfig {
	if client, ok := a.Clients["macos"]; ok {
		return &client
	}
	return nil
}

// GetDefaultScopes returns the default scopes for the configured client
// Priority: native → web → macos
func (a *Auth0Config) GetDefaultScopes() []string {
	// Try native client first (most common for CLI)
	if client := a.GetNativeClient(); client != nil && len(client.DefaultScopes) > 0 {
		return client.DefaultScopes
	}

	// Fall back to web client
	if client := a.GetWebClient(); client != nil && len(client.DefaultScopes) > 0 {
		return client.DefaultScopes
	}

	// Fall back to macOS client
	if client := a.GetMacOSClient(); client != nil && len(client.DefaultScopes) > 0 {
		return client.DefaultScopes
	}

	// Default fallback
	return []string{"openid", "profile", "email", "offline_access"}
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		APIBaseURL:     "http://localhost:8081",
		Debug:          false,
		DevMode:        false,
		LogLevel:       "info",
		AllowedOrigins: []string{}, // Empty = allow all (dev mode), must configure for production
		Workspace: WorkspaceConfig{
			Roots: []string{},
		},
		Allowlist: []string{},
	}
}

// SessionTTL returns the session time-to-live duration
// This should match or be slightly less than the server's session TTL
func SessionTTL() time.Duration {
	return 23 * time.Hour // Server uses 24h, we use 23h for safety margin
}

// SessionRefreshBuffer is the time before expiry when we should refresh
func SessionRefreshBuffer() time.Duration {
	return 1 * time.Minute
}

// TokenExpiryBuffer is the time before expiry when tokens should be refreshed
func TokenExpiryBuffer() time.Duration {
	return 5 * time.Minute
}
