package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/erauner12/toolbridge-api/internal/mcpserver/auth"
)

// mockTokenProvider is a simple mock for testing
type mockTokenProvider struct {
	token          *auth.TokenResult
	err            error
	invalidateCalls int
}

func (m *mockTokenProvider) GetToken(ctx context.Context, audience, scope string, interactive bool) (*auth.TokenResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.token, nil
}

func (m *mockTokenProvider) InvalidateToken(audience, scope string) {
	m.invalidateCalls++
}

func TestSessionManager_CreateSession(t *testing.T) {
	// Create mock server that returns a session
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sync/sessions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("unexpected method: %s", r.Method)
		}

		// Check Authorization header
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Errorf("unexpected auth header: %s", auth)
		}

		// Return session response
		w.Header().Set("X-Sync-Epoch", "42")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"id":        "test-session-123",
			"userId":    "user-456",
			"epoch":     42,
			"createdAt": time.Now().Format(time.RFC3339),
			"expiresAt": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		})
	}))
	defer server.Close()

	// Create mock token provider
	tokenProvider := &mockTokenProvider{
		token: &auth.TokenResult{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(1 * time.Hour),
			TokenType:   "Bearer",
		},
	}

	// Create session manager
	mgr := NewSessionManager(server.URL, tokenProvider, "test-audience")

	// Ensure session
	ctx := context.Background()
	session, err := mgr.EnsureSession(ctx)
	if err != nil {
		t.Fatalf("failed to ensure session: %v", err)
	}

	// Verify session
	if session.ID != "test-session-123" {
		t.Errorf("unexpected session ID: %s", session.ID)
	}
	if session.Epoch != 42 {
		t.Errorf("unexpected epoch: %d", session.Epoch)
	}
}

func TestSessionManager_CacheSession(t *testing.T) {
	callCount := 0

	// Create mock server that counts requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("X-Sync-Epoch", "1")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"id":        "session-1",
			"userId":    "user-1",
			"epoch":     1,
			"createdAt": time.Now().Format(time.RFC3339),
			"expiresAt": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		})
	}))
	defer server.Close()

	tokenProvider := &mockTokenProvider{
		token: &auth.TokenResult{AccessToken: "test-token"},
	}

	mgr := NewSessionManager(server.URL, tokenProvider, "test-audience")
	ctx := context.Background()

	// First call - should create session
	session1, err := mgr.EnsureSession(ctx)
	if err != nil {
		t.Fatalf("failed to ensure session: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}

	// Second call - should use cached session
	session2, err := mgr.EnsureSession(ctx)
	if err != nil {
		t.Fatalf("failed to ensure session: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected cached session (1 API call), got %d calls", callCount)
	}

	if session1.ID != session2.ID {
		t.Errorf("expected same session ID, got %s != %s", session1.ID, session2.ID)
	}
}

func TestSessionManager_InvalidateSession(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("X-Sync-Epoch", "1")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"id":        "session-1",
			"userId":    "user-1",
			"epoch":     1,
			"createdAt": time.Now().Format(time.RFC3339),
			"expiresAt": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		})
	}))
	defer server.Close()

	tokenProvider := &mockTokenProvider{
		token: &auth.TokenResult{AccessToken: "test-token"},
	}

	mgr := NewSessionManager(server.URL, tokenProvider, "test-audience")
	ctx := context.Background()

	// Create session
	_, err := mgr.EnsureSession(ctx)
	if err != nil {
		t.Fatalf("failed to ensure session: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}

	// Invalidate session
	mgr.InvalidateSession()

	// Next call should create new session
	_, err = mgr.EnsureSession(ctx)
	if err != nil {
		t.Fatalf("failed to ensure session after invalidation: %v", err)
	}

	if callCount != 2 {
		t.Errorf("expected 2 API calls after invalidation, got %d", callCount)
	}
}

func TestSessionManager_ThreadSafety(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(10 * time.Millisecond)
		w.Header().Set("X-Sync-Epoch", "1")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"id":        "session-1",
			"userId":    "user-1",
			"epoch":     1,
			"createdAt": time.Now().Format(time.RFC3339),
			"expiresAt": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		})
	}))
	defer server.Close()

	tokenProvider := &mockTokenProvider{
		token: &auth.TokenResult{AccessToken: "test-token"},
	}

	mgr := NewSessionManager(server.URL, tokenProvider, "test-audience")
	ctx := context.Background()

	// Launch multiple concurrent requests
	const numGoroutines = 10
	sessionChan := make(chan *Session, numGoroutines)
	errChan := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			session, err := mgr.EnsureSession(ctx)
			if err != nil {
				errChan <- err
				return
			}
			sessionChan <- session
		}()
	}

	// Collect results
	var sessions []*Session
	for i := 0; i < numGoroutines; i++ {
		select {
		case session := <-sessionChan:
			sessions = append(sessions, session)
		case err := <-errChan:
			t.Fatalf("goroutine failed: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for goroutines")
		}
	}

	// Verify all goroutines got the same session
	firstID := sessions[0].ID
	for i, session := range sessions {
		if session.ID != firstID {
			t.Errorf("goroutine %d got different session: %s != %s", i, session.ID, firstID)
		}
	}
}
