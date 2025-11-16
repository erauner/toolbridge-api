package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/erauner12/toolbridge-api/internal/mcpserver/auth"
)

func TestHTTPClient_HeaderInjection(t *testing.T) {
	var capturedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tokenProvider := &mockTokenProvider{
		token: &auth.TokenResult{AccessToken: "test-token-123"},
	}

	// Create mock session manager
	sessionMgr := &mockSessionManager{
		session: &Session{ID: "session-456", Epoch: 99},
	}

	client := NewHTTPClient(server.URL, tokenProvider, sessionMgr, "test-audience", "")

	req, _ := http.NewRequest("GET", server.URL+"/test", nil)
	_, err := client.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	// Verify headers
	if auth := capturedHeaders.Get("Authorization"); auth != "Bearer test-token-123" {
		t.Errorf("unexpected Authorization header: %s", auth)
	}
	if session := capturedHeaders.Get("X-Sync-Session"); session != "session-456" {
		t.Errorf("unexpected X-Sync-Session header: %s", session)
	}
	if epoch := capturedHeaders.Get("X-Sync-Epoch"); epoch != "99" {
		t.Errorf("unexpected X-Sync-Epoch header: %s", epoch)
	}
	if corr := capturedHeaders.Get("X-Correlation-ID"); corr == "" {
		t.Error("missing X-Correlation-ID header")
	}
}

func TestHTTPClient_DevMode(t *testing.T) {
	var capturedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Dev mode: nil tokenProvider, mock session manager, debugSub provided
	sessionMgr := &mockSessionManager{
		session: &Session{ID: "dev-session-1", Epoch: 1},
	}
	client := NewHTTPClient(server.URL, nil, sessionMgr, "", "dev-user-123")

	req, _ := http.NewRequest("GET", server.URL+"/test", nil)
	_, err := client.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	// Verify dev mode header
	if debugSub := capturedHeaders.Get("X-Debug-Sub"); debugSub != "dev-user-123" {
		t.Errorf("unexpected X-Debug-Sub header: %s", debugSub)
	}

	// Should NOT have Authorization header in dev mode
	if auth := capturedHeaders.Get("Authorization"); auth != "" {
		t.Errorf("unexpected Authorization header in dev mode: %s", auth)
	}

	// Should still have session headers
	if session := capturedHeaders.Get("X-Sync-Session"); session != "dev-session-1" {
		t.Errorf("unexpected X-Sync-Session header: %s", session)
	}
	if epoch := capturedHeaders.Get("X-Sync-Epoch"); epoch != "1" {
		t.Errorf("unexpected X-Sync-Epoch header: %s", epoch)
	}
}

func TestHTTPClient_Retry401(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: return 401
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Second call: return success
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tokenProvider := &mockTokenProvider{
		token: &auth.TokenResult{AccessToken: "fresh-token"},
	}

	sessionMgr := &mockSessionManager{
		session: &Session{ID: "session-1", Epoch: 1},
	}

	client := NewHTTPClient(server.URL, tokenProvider, sessionMgr, "test-audience", "")

	req, _ := http.NewRequest("GET", server.URL+"/test", nil)
	resp, err := client.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after retry, got %d", resp.StatusCode)
	}

	if callCount != 2 {
		t.Errorf("expected 2 API calls (401 + retry), got %d", callCount)
	}

	if tokenProvider.invalidateCalls != 1 {
		t.Errorf("expected 1 token invalidation, got %d", tokenProvider.invalidateCalls)
	}
}

func TestHTTPClient_Retry409EpochMismatch(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: return 409 with epoch mismatch
			w.Header().Set("X-Sync-Epoch", "99")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]any{
				"error": "epoch_mismatch",
				"epoch": 99,
			})
			return
		}
		// Second call: return success
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tokenProvider := &mockTokenProvider{
		token: &auth.TokenResult{AccessToken: "test-token"},
	}

	sessionMgr := &mockSessionManager{
		session: &Session{ID: "session-1", Epoch: 1},
	}

	client := NewHTTPClient(server.URL, tokenProvider, sessionMgr, "test-audience", "")

	req, _ := http.NewRequest("GET", server.URL+"/test", nil)
	resp, err := client.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after retry, got %d", resp.StatusCode)
	}

	if callCount != 2 {
		t.Errorf("expected 2 API calls (409 + retry), got %d", callCount)
	}

	if sessionMgr.invalidateCalls != 1 {
		t.Errorf("expected 1 session invalidation, got %d", sessionMgr.invalidateCalls)
	}
}

func TestHTTPClient_Retry429(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: return 429 with Retry-After
			w.Header().Set("Retry-After", "1") // 1 second
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		// Second call: return success
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tokenProvider := &mockTokenProvider{
		token: &auth.TokenResult{AccessToken: "test-token"},
	}

	sessionMgr := &mockSessionManager{
		session: &Session{ID: "session-1", Epoch: 1},
	}

	client := NewHTTPClient(server.URL, tokenProvider, sessionMgr, "test-audience", "")

	req, _ := http.NewRequest("GET", server.URL+"/test", nil)
	start := time.Now()
	resp, err := client.Do(context.Background(), req)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after retry, got %d", resp.StatusCode)
	}

	if callCount != 2 {
		t.Errorf("expected 2 API calls (429 + retry), got %d", callCount)
	}

	// Should have waited at least 1 second
	if duration < 1*time.Second {
		t.Errorf("expected backoff of at least 1s, got %v", duration)
	}
}

func TestHTTPClient_Retry428(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: return 428 (session missing/expired)
			w.WriteHeader(http.StatusPreconditionRequired)
			return
		}
		// Second call: return success
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tokenProvider := &mockTokenProvider{
		token: &auth.TokenResult{AccessToken: "test-token"},
	}

	sessionMgr := &mockSessionManager{
		session: &Session{ID: "session-1", Epoch: 1},
	}

	client := NewHTTPClient(server.URL, tokenProvider, sessionMgr, "test-audience", "")

	req, _ := http.NewRequest("GET", server.URL+"/test", nil)
	resp, err := client.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after retry, got %d", resp.StatusCode)
	}

	if callCount != 2 {
		t.Errorf("expected 2 API calls (428 + retry), got %d", callCount)
	}

	// Session should have been invalidated
	if sessionMgr.invalidateCalls != 1 {
		t.Errorf("expected 1 session invalidation, got %d", sessionMgr.invalidateCalls)
	}
}

func TestHTTPClient_MaxRetries(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// Always return 401
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	tokenProvider := &mockTokenProvider{
		token: &auth.TokenResult{AccessToken: "test-token"},
	}

	sessionMgr := &mockSessionManager{
		session: &Session{ID: "session-1", Epoch: 1},
	}

	client := NewHTTPClient(server.URL, tokenProvider, sessionMgr, "test-audience", "")

	req, _ := http.NewRequest("GET", server.URL+"/test", nil)
	_, err := client.Do(context.Background(), req)

	// Should fail after max retries
	if err == nil {
		t.Fatal("expected error after max retries")
	}

	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("unexpected error: %v", err)
	}

	// Should have tried MaxRetries + 1 times
	expectedCalls := MaxRetries + 1
	if callCount != expectedCalls {
		t.Errorf("expected %d calls (initial + %d retries), got %d", expectedCalls, MaxRetries, callCount)
	}
}

func TestHTTPClient_RequestCloning(t *testing.T) {
	callCount := 0
	var capturedBodies []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// Read body
		bodyBytes, _ := io.ReadAll(r.Body)
		capturedBodies = append(capturedBodies, string(bodyBytes))

		if callCount == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tokenProvider := &mockTokenProvider{
		token: &auth.TokenResult{AccessToken: "test-token"},
	}

	sessionMgr := &mockSessionManager{
		session: &Session{ID: "session-1", Epoch: 1},
	}

	client := NewHTTPClient(server.URL, tokenProvider, sessionMgr, "test-audience", "")

	reqBody := `{"test":"data"}`
	req, _ := http.NewRequest("POST", server.URL+"/test", strings.NewReader(reqBody))
	_, err := client.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	// Both requests should have the same body
	if len(capturedBodies) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(capturedBodies))
	}

	if capturedBodies[0] != reqBody {
		t.Errorf("first request body incorrect: %s", capturedBodies[0])
	}

	if capturedBodies[1] != reqBody {
		t.Errorf("second request body incorrect (body not preserved on retry): %s", capturedBodies[1])
	}
}

// Mock session manager for testing
type mockSessionManager struct {
	session         *Session
	invalidateCalls int
}

func (m *mockSessionManager) EnsureSession(ctx context.Context) (*Session, error) {
	return m.session, nil
}

func (m *mockSessionManager) InvalidateSession() {
	m.invalidateCalls++
}
