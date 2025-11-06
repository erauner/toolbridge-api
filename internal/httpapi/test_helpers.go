package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// createTestSession creates a sync session for testing and returns the session ID
func createTestSession(t *testing.T, router http.Handler) string {
	t.Helper()

	req := httptest.NewRequest("POST", "/v1/sync/sessions", nil)
	req.Header.Set("X-Debug-Sub", "test-user")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Fatalf("Failed to create session: got status %d, body: %s", w.Code, w.Body.String())
	}

	var session struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&session); err != nil {
		t.Fatalf("Failed to decode session response: %v", err)
	}

	return session.ID
}

// makeRequestWithSession makes an HTTP request with X-Sync-Session header
func makeRequestWithSession(t *testing.T, router http.Handler, method, path string, body interface{}, sessionID string) *httptest.ResponseRecorder {
	t.Helper()

	var bodyReader *bytes.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("Failed to marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	} else {
		bodyReader = bytes.NewReader([]byte{})
	}

	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Debug-Sub", "test-user")
	req.Header.Set("X-Sync-Session", sessionID)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	return w
}
