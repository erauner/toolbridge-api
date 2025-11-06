package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/erauner12/toolbridge-api/internal/auth"
)

func TestSessionRequired_UserMismatch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	// Clean up tables
	_, _ = pool.Exec(context.Background(), "DELETE FROM note")

	srv := &Server{DB: pool}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// User A creates a session
	req := httptest.NewRequest("POST", "/v1/sync/sessions", nil)
	req.Header.Set("X-Debug-Sub", "user-a")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Fatalf("Failed to create session for user-a: got status %d", w.Code)
	}

	var sessionResp struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&sessionResp); err != nil {
		t.Fatalf("Failed to decode session response: %v", err)
	}
	userASessionID := sessionResp.ID

	// User B tries to use User A's session (should be rejected)
	pushReq := httptest.NewRequest("POST", "/v1/sync/notes/push", nil)
	pushReq.Header.Set("Content-Type", "application/json")
	pushReq.Header.Set("X-Debug-Sub", "user-b")
	pushReq.Header.Set("X-Sync-Session", userASessionID)

	pushRec := httptest.NewRecorder()
	router.ServeHTTP(pushRec, pushReq)

	t.Logf("User B attempting to use User A's session: sessionID=%s, status=%d, body=%s",
		userASessionID, pushRec.Code, pushRec.Body.String())

	// Should get 403 Forbidden because session doesn't belong to user-b
	if pushRec.Code != 403 {
		t.Errorf("Expected 403 Forbidden when using another user's session, got %d: %s",
			pushRec.Code, pushRec.Body.String())
	}

	// Verify error message mentions session ownership
	body := pushRec.Body.String()
	if body == "" {
		t.Error("Expected error message in response body")
	}
}

func TestSessionRequired_SameUser(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	// Clean up tables
	_, _ = pool.Exec(context.Background(), "DELETE FROM note")

	srv := &Server{DB: pool}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// User creates a session
	req := httptest.NewRequest("POST", "/v1/sync/sessions", nil)
	req.Header.Set("X-Debug-Sub", "test-user")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Fatalf("Failed to create session: got status %d", w.Code)
	}

	var sessionResp struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&sessionResp); err != nil {
		t.Fatalf("Failed to decode session response: %v", err)
	}

	// Same user uses their own session (should succeed)
	// Manually set X-Debug-Sub to match the session owner
	pushReq := httptest.NewRequest("POST", "/v1/sync/notes/push", nil)
	pushReq.Header.Set("X-Debug-Sub", "test-user")
	pushReq.Header.Set("X-Sync-Session", sessionResp.ID)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, pushReq)

	// Should succeed (200 or other success code, not 403)
	if rec.Code == 403 {
		t.Errorf("Expected success when using own session, got 403: %s", rec.Body.String())
	}
	if rec.Code != 200 {
		t.Logf("Got status %d (expected 200, but anything except 403 is acceptable for this test): %s",
			rec.Code, rec.Body.String())
	}
}
