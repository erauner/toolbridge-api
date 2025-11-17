package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	// Default redirect URIs
	defaultWebRedirectURI = "http://localhost:5173/oauth_callback.html"
)

// defaultRedirectURI generates platform-specific redirect URIs
func defaultIosRedirectURI(domain string) string {
	return fmt.Sprintf("com.erauner.toolbridge://%s/ios/com.erauner.toolbridge/callback", domain)
}

func defaultAndroidRedirectURI(domain string) string {
	return fmt.Sprintf("com.erauner.toolbridge://%s/android/com.erauner.toolbridge/callback", domain)
}

func defaultMacosRedirectURI(domain string) string {
	return fmt.Sprintf("run.erauner.toolbridge://%s/macos/run.erauner.toolbridge/callback", domain)
}

// Load loads configuration from a file path and applies environment variable overrides
// Validation is deferred to allow CLI flag overrides to be applied first
func Load(configPath string) (*Config, error) {
	// Start with default config
	cfg := DefaultConfig()

	// If config path is provided, load from file
	if configPath != "" {
		fileConfig, err := loadFromFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load config from file: %w", err)
		}
		cfg = fileConfig
	}

	// Apply environment variable overrides
	applyEnvironmentOverrides(cfg)

	// Note: Validation is NOT performed here to allow CLI flags to override
	// Call cfg.Validate() after applying CLI overrides in the caller

	return cfg, nil
}

// loadFromFile loads configuration from a JSON file
func loadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrConfigFileNotFound
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfigFormat, err)
	}

	// Apply template substitutions for redirect URIs
	if cfg.Auth0.Domain != "" {
		substituteRedirectTemplates(&cfg.Auth0)
	}

	// Set defaults for scopes if not provided
	applyDefaultScopes(&cfg.Auth0)

	return &cfg, nil
}

// applyEnvironmentOverrides applies configuration from environment variables
func applyEnvironmentOverrides(cfg *Config) {
	// API Base URL
	if apiURL := os.Getenv("MCP_API_BASE_URL"); apiURL != "" {
		cfg.APIBaseURL = apiURL
	}

	// Public URL (for OAuth metadata)
	if publicURL := os.Getenv("MCP_PUBLIC_URL"); publicURL != "" {
		cfg.PublicURL = publicURL
	}

	// Dev mode
	if devMode := os.Getenv("MCP_DEV_MODE"); devMode == "true" || devMode == "1" {
		cfg.DevMode = true
	}

	// Debug mode
	if debug := os.Getenv("MCP_DEBUG"); debug == "true" || debug == "1" {
		cfg.Debug = true
	}

	// Log level
	if logLevel := os.Getenv("MCP_LOG_LEVEL"); logLevel != "" {
		cfg.LogLevel = logLevel
	}

	// Allowed origins (comma-separated list)
	if allowedOrigins := os.Getenv("MCP_ALLOWED_ORIGINS"); allowedOrigins != "" {
		// Split by comma and trim whitespace
		origins := strings.Split(allowedOrigins, ",")
		cfg.AllowedOrigins = make([]string, 0, len(origins))
		for _, origin := range origins {
			trimmed := strings.TrimSpace(origin)
			if trimmed != "" {
				cfg.AllowedOrigins = append(cfg.AllowedOrigins, trimmed)
			}
		}
	}

	// Auth0 configuration from environment
	if domain := os.Getenv("AUTH0_DOMAIN"); domain != "" {
		cfg.Auth0.Domain = domain
	}

	// Web client
	if clientID := os.Getenv("AUTH0_CLIENT_ID_WEB"); clientID != "" {
		if cfg.Auth0.Clients == nil {
			cfg.Auth0.Clients = make(map[string]ClientConfig)
		}
		webClient := cfg.Auth0.Clients["web"]
		webClient.ClientID = clientID
		if webClient.RedirectURI == "" {
			webClient.RedirectURI = defaultWebRedirectURI
		}
		cfg.Auth0.Clients["web"] = webClient
	}

	// Native client
	if clientID := os.Getenv("AUTH0_CLIENT_ID_NATIVE"); clientID != "" {
		if cfg.Auth0.Clients == nil {
			cfg.Auth0.Clients = make(map[string]ClientConfig)
		}
		nativeClient := cfg.Auth0.Clients["native"]
		nativeClient.ClientID = clientID
		if nativeClient.RedirectURI == "" && cfg.Auth0.Domain != "" {
			nativeClient.RedirectURI = defaultIosRedirectURI(cfg.Auth0.Domain)
			if nativeClient.RedirectURIOverrides == nil {
				nativeClient.RedirectURIOverrides = make(map[string]string)
			}
			nativeClient.RedirectURIOverrides["android"] = defaultAndroidRedirectURI(cfg.Auth0.Domain)
		}
		cfg.Auth0.Clients["native"] = nativeClient
	}

	// macOS client
	if clientID := os.Getenv("AUTH0_CLIENT_ID_NATIVE_MACOS"); clientID != "" {
		if cfg.Auth0.Clients == nil {
			cfg.Auth0.Clients = make(map[string]ClientConfig)
		}
		macosClient := cfg.Auth0.Clients["macos"]
		macosClient.ClientID = clientID
		if macosClient.RedirectURI == "" && cfg.Auth0.Domain != "" {
			macosClient.RedirectURI = defaultMacosRedirectURI(cfg.Auth0.Domain)
		}
		cfg.Auth0.Clients["macos"] = macosClient
	}

	// Sync API audience
	if audience := os.Getenv("AUTH0_SYNC_API_AUDIENCE"); audience != "" {
		if cfg.Auth0.SyncAPI == nil {
			cfg.Auth0.SyncAPI = &SyncAPIConfig{}
		}
		cfg.Auth0.SyncAPI.Audience = audience
	}

	// Sync API scope
	if scope := os.Getenv("AUTH0_SYNC_API_SCOPE"); scope != "" {
		if cfg.Auth0.SyncAPI == nil {
			cfg.Auth0.SyncAPI = &SyncAPIConfig{}
		}
		cfg.Auth0.SyncAPI.Scope = scope
	}

	// Token introspection configuration
	introspectionClientID := strings.TrimSpace(os.Getenv("AUTH0_INTROSPECTION_CLIENT_ID"))
	introspectionClientSecret := strings.TrimSpace(os.Getenv("AUTH0_INTROSPECTION_CLIENT_SECRET"))
	introspectionAudience := strings.TrimSpace(os.Getenv("AUTH0_INTROSPECTION_AUDIENCE"))

	// Create introspection config if either clientID or clientSecret is provided
	if introspectionClientID != "" || introspectionClientSecret != "" {
		if cfg.Auth0.Introspection == nil {
			cfg.Auth0.Introspection = &IntrospectionConfig{}
		}
		if introspectionClientID != "" {
			cfg.Auth0.Introspection.ClientID = introspectionClientID
		}
		if introspectionClientSecret != "" {
			cfg.Auth0.Introspection.ClientSecret = introspectionClientSecret
		}
		if introspectionAudience != "" {
			cfg.Auth0.Introspection.Audience = introspectionAudience
		}
	}

	// Apply default scopes after all overrides
	applyDefaultScopes(&cfg.Auth0)
}

// substituteRedirectTemplates replaces {{domain}} placeholders in redirect URIs
func substituteRedirectTemplates(auth0 *Auth0Config) {
	for key, client := range auth0.Clients {
		if client.RedirectURI != "" {
			client.RedirectURI = strings.ReplaceAll(client.RedirectURI, "{{domain}}", auth0.Domain)
		}

		for platform, uri := range client.RedirectURIOverrides {
			client.RedirectURIOverrides[platform] = strings.ReplaceAll(uri, "{{domain}}", auth0.Domain)
		}

		auth0.Clients[key] = client
	}
}

// applyDefaultScopes sets default scopes for clients that don't have them
func applyDefaultScopes(auth0 *Auth0Config) {
	defaultScopes := []string{"openid", "profile", "email", "offline_access"}

	for key, client := range auth0.Clients {
		if len(client.DefaultScopes) == 0 {
			client.DefaultScopes = defaultScopes
		}
		auth0.Clients[key] = client
	}
}

// LoadFromEnvironment creates a configuration using only environment variables
// This is useful for containerized deployments where files may not be available
// Validation is deferred to allow CLI flag overrides to be applied first
func LoadFromEnvironment() (*Config, error) {
	cfg := DefaultConfig()
	applyEnvironmentOverrides(cfg)

	// Note: Validation is NOT performed here to allow CLI flags to override
	// Call cfg.Validate() after applying CLI overrides in the caller

	return cfg, nil
}
