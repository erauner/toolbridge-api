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

	// Validate the final configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

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
func LoadFromEnvironment() (*Config, error) {
	cfg := DefaultConfig()
	applyEnvironmentOverrides(cfg)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return cfg, nil
}
