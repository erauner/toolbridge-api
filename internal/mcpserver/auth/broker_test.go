package auth

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/erauner12/toolbridge-api/internal/mcpserver/config"
)

// mockDelegate is a simple delegate implementation for testing
type mockDelegate struct {
	tokens map[string]*TokenResult
	err    error
}

func (m *mockDelegate) Configure(cfg config.Auth0Config) error {
	return m.err
}

func (m *mockDelegate) EnsureSession(ctx context.Context, interactive bool, scopes []string) (bool, error) {
	return m.err == nil, m.err
}

func (m *mockDelegate) TryGetToken(ctx context.Context, audience string, scopes []string, interactive bool) (*TokenResult, error) {
	if m.err != nil {
		return nil, m.err
	}

	key := audience + "::" + join(scopes)
	if token, ok := m.tokens[key]; ok {
		return token, nil
	}

	// Return a default token
	return &TokenResult{
		AccessToken: "mock-token",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		TokenType:   "Bearer",
	}, nil
}

func (m *mockDelegate) TryGetIDToken(ctx context.Context, scopes []string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return "mock-id-token", nil
}

func (m *mockDelegate) LogoutAll(ctx context.Context) error {
	return m.err
}

func join(scopes []string) string {
	sorted := make([]string, len(scopes))
	copy(sorted, scopes)
	sort.Strings(sorted)
	result := ""
	for i, s := range sorted {
		if i > 0 {
			result += " "
		}
		result += s
	}
	return result
}

func TestBroker_MergeScopes(t *testing.T) {
	tests := []struct {
		name          string
		defaultScopes []string
		userScope     string
		expected      []string
	}{
		{
			name:          "no user scopes",
			defaultScopes: []string{"openid", "profile"},
			userScope:     "",
			expected:      []string{"openid", "profile"},
		},
		{
			name:          "additional user scopes",
			defaultScopes: []string{"openid", "profile"},
			userScope:     "email offline_access",
			expected:      []string{"email", "offline_access", "openid", "profile"},
		},
		{
			name:          "duplicate scopes",
			defaultScopes: []string{"openid", "profile"},
			userScope:     "profile email",
			expected:      []string{"email", "openid", "profile"},
		},
		{
			name:          "empty default scopes",
			defaultScopes: []string{},
			userScope:     "custom:scope",
			expected:      []string{"custom:scope"},
		},
		{
			name:          "sorted output",
			defaultScopes: []string{"z-scope", "a-scope"},
			userScope:     "m-scope",
			expected:      []string{"a-scope", "m-scope", "z-scope"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Auth0Config{
				Domain: "test.auth0.com",
				Clients: map[string]config.ClientConfig{
					"native": {
						ClientID:        "test-client",
						DefaultScopes:   tt.defaultScopes,
						RedirectURI:     "http://localhost",
						AdditionalParams: map[string]string{},
					},
				},
			}

			broker := &TokenBroker{
				config:        cfg,
				delegate:      &mockDelegate{},
				cache:         make(map[string]*CachedToken),
				defaultScopes: tt.defaultScopes,
			}

			result := broker.mergeScopes(tt.userScope)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d scopes, got %d: %v", len(tt.expected), len(result), result)
				return
			}

			for i, scope := range result {
				if scope != tt.expected[i] {
					t.Errorf("scope[%d]: expected %q, got %q", i, tt.expected[i], scope)
				}
			}
		})
	}
}

func TestBroker_CacheKey(t *testing.T) {
	broker := &TokenBroker{}

	tests := []struct {
		name     string
		audience string
		scopes   []string
		expected string
	}{
		{
			name:     "simple case",
			audience: "https://api.example.com",
			scopes:   []string{"openid", "profile"},
			expected: "https://api.example.com::openid profile",
		},
		{
			name:     "empty audience",
			audience: "",
			scopes:   []string{"openid"},
			expected: "::openid",
		},
		{
			name:     "no scopes",
			audience: "https://api.example.com",
			scopes:   []string{},
			expected: "https://api.example.com::",
		},
		{
			name:     "multiple scopes",
			audience: "aud1",
			scopes:   []string{"a", "b", "c"},
			expected: "aud1::a b c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := broker.cacheKey(tt.audience, tt.scopes)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestBroker_IsExpiring(t *testing.T) {
	broker := &TokenBroker{}

	tests := []struct {
		name     string
		expiresAt time.Time
		expected bool
	}{
		{
			name:      "expires in 10 minutes - not expiring",
			expiresAt: time.Now().Add(10 * time.Minute),
			expected:  false,
		},
		{
			name:      "expires in 3 minutes - expiring",
			expiresAt: time.Now().Add(3 * time.Minute),
			expected:  true,
		},
		{
			name:      "expires in 5 minutes exactly - expiring",
			expiresAt: time.Now().Add(ExpiryBuffer),
			expected:  true,
		},
		{
			name:      "already expired",
			expiresAt: time.Now().Add(-1 * time.Minute),
			expected:  true,
		},
		{
			name:      "expires in 1 hour - not expiring",
			expiresAt: time.Now().Add(1 * time.Hour),
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := &TokenResult{
				AccessToken: "test",
				ExpiresAt:   tt.expiresAt,
				TokenType:   "Bearer",
			}

			result := broker.isExpiring(token)
			if result != tt.expected {
				t.Errorf("expected %v, got %v (time until expiry: %v)", tt.expected, result, time.Until(tt.expiresAt))
			}
		})
	}
}

func TestBroker_GetToken_Caching(t *testing.T) {
	cfg := config.Auth0Config{
		Domain: "test.auth0.com",
		Clients: map[string]config.ClientConfig{
			"native": {
				ClientID:      "test-client",
				DefaultScopes: []string{"openid", "profile"},
			},
		},
	}

	delegate := &mockDelegate{
		tokens: make(map[string]*TokenResult),
	}

	broker, err := NewBroker(cfg, delegate)
	if err != nil {
		t.Fatalf("failed to create broker: %v", err)
	}

	ctx := context.Background()
	audience := "https://api.example.com"

	// First call - should acquire new token
	token1, err := broker.GetToken(ctx, audience, "", true)
	if err != nil {
		t.Fatalf("GetToken failed: %v", err)
	}
	if token1 == nil {
		t.Fatal("expected non-nil token")
	}

	// Second call - should return cached token (same instance)
	token2, err := broker.GetToken(ctx, audience, "", true)
	if err != nil {
		t.Fatalf("GetToken failed: %v", err)
	}
	if token2 == nil {
		t.Fatal("expected non-nil token")
	}

	if token1.AccessToken != token2.AccessToken {
		t.Errorf("expected cached token to have same access token")
	}
}

func TestBroker_InvalidateToken(t *testing.T) {
	cfg := config.Auth0Config{
		Domain: "test.auth0.com",
		Clients: map[string]config.ClientConfig{
			"native": {
				ClientID:      "test-client",
				DefaultScopes: []string{"openid"},
			},
		},
	}

	delegate := &mockDelegate{}
	broker, err := NewBroker(cfg, delegate)
	if err != nil {
		t.Fatalf("failed to create broker: %v", err)
	}

	ctx := context.Background()
	audience := "https://api.example.com"

	// Get token to populate cache
	_, err = broker.GetToken(ctx, audience, "", true)
	if err != nil {
		t.Fatalf("GetToken failed: %v", err)
	}

	// Verify token is cached
	cacheKey := broker.cacheKey(audience, broker.mergeScopes(""))
	if broker.getCached(cacheKey) == nil {
		t.Fatal("expected token to be cached")
	}

	// Invalidate token
	broker.InvalidateToken(audience, "")

	// Verify token is no longer cached
	if broker.getCached(cacheKey) != nil {
		t.Fatal("expected token to be removed from cache")
	}
}

func TestBroker_ThreadSafety(t *testing.T) {
	cfg := config.Auth0Config{
		Domain: "test.auth0.com",
		Clients: map[string]config.ClientConfig{
			"native": {
				ClientID:      "test-client",
				DefaultScopes: []string{"openid"},
			},
		},
	}

	delegate := &mockDelegate{}
	broker, err := NewBroker(cfg, delegate)
	if err != nil {
		t.Fatalf("failed to create broker: %v", err)
	}

	ctx := context.Background()

	// Run concurrent operations
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			audience := "https://api.example.com"
			_, _ = broker.GetToken(ctx, audience, "", true)
			broker.InvalidateToken(audience, "")
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}
