package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromEnvironment(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		wantErr bool
		checks  func(*testing.T, *Config)
	}{
		{
			name: "minimal valid config with dev mode",
			envVars: map[string]string{
				"MCP_API_BASE_URL": "http://localhost:8081",
				"MCP_DEV_MODE":     "true",
			},
			wantErr: false,
			checks: func(t *testing.T, cfg *Config) {
				if cfg.APIBaseURL != "http://localhost:8081" {
					t.Errorf("expected APIBaseURL=http://localhost:8081, got %s", cfg.APIBaseURL)
				}
				if !cfg.DevMode {
					t.Error("expected DevMode=true")
				}
			},
		},
		{
			name: "auth0 configuration from env",
			envVars: map[string]string{
				"MCP_API_BASE_URL":        "http://localhost:8081",
				"AUTH0_DOMAIN":            "test.auth0.com",
				"AUTH0_CLIENT_ID_NATIVE":  "native-client-id",
				"AUTH0_SYNC_API_AUDIENCE": "https://api.example.com",
			},
			wantErr: false,
			checks: func(t *testing.T, cfg *Config) {
				if cfg.Auth0.Domain != "test.auth0.com" {
					t.Errorf("expected Auth0.Domain=test.auth0.com, got %s", cfg.Auth0.Domain)
				}
				if cfg.Auth0.Clients["native"].ClientID != "native-client-id" {
					t.Error("expected native client to be configured")
				}
				if cfg.Auth0.SyncAPI.Audience != "https://api.example.com" {
					t.Errorf("expected SyncAPI.Audience=https://api.example.com, got %s", cfg.Auth0.SyncAPI.Audience)
				}
			},
		},
		{
			name: "default values when no env set",
			envVars: map[string]string{
				"MCP_DEV_MODE": "true", // Required to pass validation
			},
			wantErr: false,
			checks: func(t *testing.T, cfg *Config) {
				if cfg.APIBaseURL != "http://localhost:8081" {
					t.Errorf("expected default APIBaseURL, got %s", cfg.APIBaseURL)
				}
				if cfg.LogLevel != "info" {
					t.Errorf("expected default LogLevel=info, got %s", cfg.LogLevel)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment
			for _, key := range []string{
				"MCP_API_BASE_URL", "MCP_DEV_MODE", "MCP_DEBUG", "MCP_LOG_LEVEL",
				"AUTH0_DOMAIN", "AUTH0_CLIENT_ID_NATIVE", "AUTH0_CLIENT_ID_WEB",
				"AUTH0_CLIENT_ID_NATIVE_MACOS", "AUTH0_SYNC_API_AUDIENCE", "AUTH0_SYNC_API_SCOPE",
			} {
				os.Unsetenv(key)
			}

			// Set test environment variables
			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}
			defer func() {
				for key := range tt.envVars {
					os.Unsetenv(key)
				}
			}()

			cfg, err := LoadFromEnvironment()
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadFromEnvironment() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err == nil && tt.checks != nil {
				tt.checks(t, cfg)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := t.TempDir()

	// Create test config file
	testConfigPath := filepath.Join(tmpDir, "test_config.json")
	testConfigJSON := `{
  "apiBaseUrl": "http://test-api:8080",
  "debug": true,
  "logLevel": "debug",
  "auth0": {
    "domain": "test.auth0.com",
    "clients": {
      "native": {
        "clientId": "test-client-id",
        "redirectUri": "com.example://{{domain}}/callback",
        "scopes": ["openid", "profile"]
      }
    },
    "syncApi": {
      "audience": "https://api.test.com"
    }
  }
}`
	if err := os.WriteFile(testConfigPath, []byte(testConfigJSON), 0644); err != nil {
		t.Fatalf("failed to create test config file: %v", err)
	}

	tests := []struct {
		name       string
		configPath string
		envVars    map[string]string
		wantErr    bool
		checks     func(*testing.T, *Config)
	}{
		{
			name:       "load from file",
			configPath: testConfigPath,
			wantErr:    false,
			checks: func(t *testing.T, cfg *Config) {
				if cfg.APIBaseURL != "http://test-api:8080" {
					t.Errorf("expected APIBaseURL from file, got %s", cfg.APIBaseURL)
				}
				if cfg.Auth0.Domain != "test.auth0.com" {
					t.Errorf("expected Auth0.Domain from file, got %s", cfg.Auth0.Domain)
				}
				// Check template substitution
				native := cfg.Auth0.Clients["native"]
				expected := "com.example://test.auth0.com/callback"
				if native.RedirectURI != expected {
					t.Errorf("expected redirect URI with domain substituted: %s, got %s", expected, native.RedirectURI)
				}
			},
		},
		{
			name:       "env overrides file",
			configPath: testConfigPath,
			envVars: map[string]string{
				"MCP_API_BASE_URL": "http://override:9000",
			},
			wantErr: false,
			checks: func(t *testing.T, cfg *Config) {
				if cfg.APIBaseURL != "http://override:9000" {
					t.Errorf("expected env to override file APIBaseURL, got %s", cfg.APIBaseURL)
				}
				// File values should still be present for non-overridden fields
				if !cfg.Debug {
					t.Error("expected Debug=true from file")
				}
			},
		},
		{
			name:       "nonexistent file",
			configPath: "/nonexistent/config.json",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}
			defer func() {
				for key := range tt.envVars {
					os.Unsetenv(key)
				}
			}()

			cfg, err := Load(tt.configPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err == nil && tt.checks != nil {
				tt.checks(t, cfg)
			}
		})
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid dev mode config",
			config: &Config{
				APIBaseURL: "http://localhost:8081",
				DevMode:    true,
				Auth0: Auth0Config{
					Domain:  "", // Empty OK in dev mode
					Clients: map[string]ClientConfig{},
				},
			},
			wantErr: false,
		},
		{
			name: "missing API base URL",
			config: &Config{
				DevMode: true,
			},
			wantErr: true,
			errMsg:  "apiBaseUrl is required in configuration",
		},
		{
			name: "missing auth0 domain in production",
			config: &Config{
				APIBaseURL: "http://localhost:8081",
				DevMode:    false, // Production mode
				Auth0: Auth0Config{
					Domain:  "", // Invalid in production
					Clients: map[string]ClientConfig{},
				},
			},
			wantErr: true,
			errMsg:  "auth0.domain is required when not in dev mode",
		},
		{
			name: "valid production config",
			config: &Config{
				APIBaseURL: "http://localhost:8081",
				DevMode:    false,
				Auth0: Auth0Config{
					Domain: "test.auth0.com",
					Clients: map[string]ClientConfig{
						"native": {
							ClientID: "test-client-id",
						},
					},
					SyncAPI: &SyncAPIConfig{
						Audience: "https://api.toolbridge.dev",
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && tt.errMsg != "" {
				if err.Error() != tt.errMsg {
					t.Errorf("Validate() error message = %q, want %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

func TestDefaultScopes(t *testing.T) {
	tests := []struct {
		name     string
		auth0    Auth0Config
		expected []string
	}{
		{
			name: "native client scopes",
			auth0: Auth0Config{
				Clients: map[string]ClientConfig{
					"native": {
						ClientID:      "native-id",
						DefaultScopes: []string{"openid", "profile", "custom"},
					},
					"web": {
						ClientID:      "web-id",
						DefaultScopes: []string{"openid", "profile"},
					},
				},
			},
			expected: []string{"openid", "profile", "custom"},
		},
		{
			name: "fallback to web when native has no scopes",
			auth0: Auth0Config{
				Clients: map[string]ClientConfig{
					"native": {
						ClientID:      "native-id",
						DefaultScopes: []string{},
					},
					"web": {
						ClientID:      "web-id",
						DefaultScopes: []string{"openid", "email"},
					},
				},
			},
			expected: []string{"openid", "email"},
		},
		{
			name: "default scopes when no clients configured",
			auth0: Auth0Config{
				Clients: map[string]ClientConfig{},
			},
			expected: []string{"openid", "profile", "email", "offline_access"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.auth0.GetDefaultScopes()
			if len(got) != len(tt.expected) {
				t.Errorf("GetDefaultScopes() length = %d, want %d", len(got), len(tt.expected))
				return
			}
			for i, scope := range tt.expected {
				if got[i] != scope {
					t.Errorf("GetDefaultScopes()[%d] = %q, want %q", i, got[i], scope)
				}
			}
		})
	}
}
