package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/erauner12/toolbridge-api/internal/service/syncservice"
	"github.com/erauner12/toolbridge-api/internal/syncx"
	"github.com/google/uuid"
)

const testUserSubject = "test-user"

// Regression: two REST mutations in the same millisecond should not clobber the winner payload.
func TestApplyNoteMutation_SameTimestamp_NoClobber(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	srv := &Server{
		DB:              pool,
		RateLimitConfig: DefaultRateLimitConfig,
		NoteSvc:         syncservice.NewNoteService(pool),
	}

	ctx := context.Background()
	userID := createTestUser(t, pool, testUserSubject)

	ts := syncx.NowMs()
	noteUID := uuid.New()

	firstPayload := map[string]any{
		"uid":     noteUID.String(),
		"title":   "First Title",
		"content": "winner payload",
	}
	secondPayload := map[string]any{
		"uid":     noteUID.String(),
		"title":   "Second Title",
		"content": "stale payload",
	}

	firstItem, err := srv.NoteSvc.ApplyNoteMutation(ctx, userID, firstPayload, syncservice.MutationOpts{
		ForceTimestampMs: &ts,
	})
	if err != nil {
		t.Fatalf("first mutation failed: %v", err)
	}

	secondItem, err := srv.NoteSvc.ApplyNoteMutation(ctx, userID, secondPayload, syncservice.MutationOpts{
		ForceTimestampMs: &ts,
	})
	if err != nil {
		t.Fatalf("second mutation failed: %v", err)
	}

	if got := secondItem.Payload["content"]; got != "winner payload" {
		t.Fatalf("expected second response payload to reflect winner, got %v", got)
	}

	current, err := srv.NoteSvc.GetNote(ctx, userID, noteUID)
	if err != nil {
		t.Fatalf("failed to reload note: %v", err)
	}
	if got := current.Payload["content"]; got != "winner payload" {
		t.Fatalf("expected DB payload to keep winner, got %v", got)
	}

	if current.Version != firstItem.Version {
		t.Fatalf("expected version unchanged; first=%d current=%d", firstItem.Version, current.Version)
	}
}

// TestGetNote_IncludeDeleted tests the includeDeleted query parameter behavior
func TestGetNote_IncludeDeleted(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	srv := &Server{
		DB:              pool,
		RateLimitConfig: DefaultRateLimitConfig,
		NoteSvc:         syncservice.NewNoteService(pool),
	}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	ctx := context.Background()
	userID := createTestUser(t, pool, testUserSubject) // Create user and get UUID

	t.Run("404_for_nonexistent_note", func(t *testing.T) {
		// Create a session to ensure the request has proper session headers
		session := createTestSession(t, router)

		nonexistentUID := uuid.New()

		req := httptest.NewRequest("GET", fmt.Sprintf("/v1/notes/%s", nonexistentUID), nil)
		req.Header.Set("X-Debug-Sub", testUserSubject)
		req.Header.Set("X-Sync-Session", session.ID)
		req.Header.Set("X-Sync-Epoch", fmt.Sprintf("%d", session.Epoch))
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected 404, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("410_Gone_for_deleted_note_without_includeDeleted", func(t *testing.T) {
		// Create a session
		session := createTestSession(t, router)

		// Create a note via REST API
		noteUID := uuid.New()
		notePayload := map[string]any{
			"uid":     noteUID.String(),
			"title":   "Test Note",
			"content": "This will be deleted",
		}

		_, err := srv.NoteSvc.ApplyNoteMutation(ctx, userID, notePayload, syncservice.MutationOpts{})
		if err != nil {
			t.Fatalf("Failed to create note: %v", err)
		}

		// Soft-delete the note
		_, err = srv.NoteSvc.ApplyNoteMutation(ctx, userID, map[string]any{
			"uid": noteUID.String(),
		}, syncservice.MutationOpts{SetDeleted: true})
		if err != nil {
			t.Fatalf("Failed to delete note: %v", err)
		}

		// Try to GET without includeDeleted flag (should get 410)
		req := httptest.NewRequest("GET", fmt.Sprintf("/v1/notes/%s", noteUID), nil)
		req.Header.Set("X-Debug-Sub", testUserSubject)
		req.Header.Set("X-Sync-Session", session.ID)
		req.Header.Set("X-Sync-Epoch", fmt.Sprintf("%d", session.Epoch))
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusGone {
			t.Errorf("Expected 410 Gone, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if resp["error"] != "note deleted" {
			t.Errorf("Expected error='note deleted', got %v", resp["error"])
		}

		if resp["deletedAt"] == nil {
			t.Error("Expected deletedAt timestamp in response")
		}
	})

	t.Run("200_OK_for_deleted_note_with_includeDeleted_true", func(t *testing.T) {
		// Create a session
		session := createTestSession(t, router)

		// Create another note
		noteUID := uuid.New()
		notePayload := map[string]any{
			"uid":     noteUID.String(),
			"title":   "Test Note 2",
			"content": "This will be deleted but retrieved",
		}

		_, err := srv.NoteSvc.ApplyNoteMutation(ctx, userID, notePayload, syncservice.MutationOpts{})
		if err != nil {
			t.Fatalf("Failed to create note: %v", err)
		}

		// Soft-delete the note
		_, err = srv.NoteSvc.ApplyNoteMutation(ctx, userID, map[string]any{
			"uid": noteUID.String(),
		}, syncservice.MutationOpts{SetDeleted: true})
		if err != nil {
			t.Fatalf("Failed to delete note: %v", err)
		}

		// GET with includeDeleted=true (should get 200 with full item)
		req := httptest.NewRequest("GET", fmt.Sprintf("/v1/notes/%s?includeDeleted=true", noteUID), nil)
		req.Header.Set("X-Debug-Sub", testUserSubject)
		req.Header.Set("X-Sync-Session", session.ID)
		req.Header.Set("X-Sync-Epoch", fmt.Sprintf("%d", session.Epoch))
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp syncservice.RESTItem
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if resp.UID != noteUID.String() {
			t.Errorf("Expected UID %s, got %s", noteUID, resp.UID)
		}

		if resp.DeletedAt == nil {
			t.Error("Expected deletedAt field to be populated")
		}

		if resp.Payload == nil {
			t.Error("Expected payload to be present")
		}
	})

	t.Run("200_OK_for_active_note", func(t *testing.T) {
		// Create a session
		session := createTestSession(t, router)

		// Create an active note
		noteUID := uuid.New()
		notePayload := map[string]any{
			"uid":     noteUID.String(),
			"title":   "Active Note",
			"content": "This is not deleted",
		}

		_, err := srv.NoteSvc.ApplyNoteMutation(ctx, userID, notePayload, syncservice.MutationOpts{})
		if err != nil {
			t.Fatalf("Failed to create note: %v", err)
		}

		// GET active note (should always return 200)
		req := httptest.NewRequest("GET", fmt.Sprintf("/v1/notes/%s", noteUID), nil)
		req.Header.Set("X-Debug-Sub", testUserSubject)
		req.Header.Set("X-Sync-Session", session.ID)
		req.Header.Set("X-Sync-Epoch", fmt.Sprintf("%d", session.Epoch))
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp syncservice.RESTItem
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if resp.UID != noteUID.String() {
			t.Errorf("Expected UID %s, got %s", noteUID, resp.UID)
		}

		if resp.DeletedAt != nil {
			t.Error("Expected deletedAt to be nil for active note")
		}
	})
}

// TestGetTask_IncludeDeleted tests the same behavior for tasks
func TestGetTask_IncludeDeleted(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	// Clean tasks table
	_, err := pool.Exec(context.Background(), "DELETE FROM task")
	if err != nil {
		t.Fatalf("Failed to clean tasks table: %v", err)
	}

	srv := &Server{
		DB:              pool,
		RateLimitConfig: DefaultRateLimitConfig,
		TaskSvc:         syncservice.NewTaskService(pool),
	}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	ctx := context.Background()
	userID := createTestUser(t, pool, testUserSubject) // Create user and get UUID

	t.Run("410_Gone_for_deleted_task", func(t *testing.T) {
		// Create a session
		session := createTestSession(t, router)

		taskUID := uuid.New()
		taskPayload := map[string]any{
			"uid":         taskUID.String(),
			"title":       "Test Task",
			"description": "This will be deleted",
		}

		_, err := srv.TaskSvc.ApplyTaskMutation(ctx, userID, taskPayload, syncservice.MutationOpts{})
		if err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}

		// Delete the task
		_, err = srv.TaskSvc.ApplyTaskMutation(ctx, userID, map[string]any{
			"uid": taskUID.String(),
		}, syncservice.MutationOpts{SetDeleted: true})
		if err != nil {
			t.Fatalf("Failed to delete task: %v", err)
		}

		// GET without includeDeleted
		req := httptest.NewRequest("GET", fmt.Sprintf("/v1/tasks/%s", taskUID), nil)
		req.Header.Set("X-Debug-Sub", testUserSubject)
		req.Header.Set("X-Sync-Session", session.ID)
		req.Header.Set("X-Sync-Epoch", fmt.Sprintf("%d", session.Epoch))
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusGone {
			t.Errorf("Expected 410 Gone, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("200_OK_for_deleted_task_with_includeDeleted", func(t *testing.T) {
		// Create a session
		session := createTestSession(t, router)

		taskUID := uuid.New()
		taskPayload := map[string]any{
			"uid":   taskUID.String(),
			"title": "Test Task 2",
		}

		_, err := srv.TaskSvc.ApplyTaskMutation(ctx, userID, taskPayload, syncservice.MutationOpts{})
		if err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}

		// Delete the task
		_, err = srv.TaskSvc.ApplyTaskMutation(ctx, userID, map[string]any{
			"uid": taskUID.String(),
		}, syncservice.MutationOpts{SetDeleted: true})
		if err != nil {
			t.Fatalf("Failed to delete task: %v", err)
		}

		// GET with includeDeleted=true
		req := httptest.NewRequest("GET", fmt.Sprintf("/v1/tasks/%s?includeDeleted=true", taskUID), nil)
		req.Header.Set("X-Debug-Sub", testUserSubject)
		req.Header.Set("X-Sync-Session", session.ID)
		req.Header.Set("X-Sync-Epoch", fmt.Sprintf("%d", session.Epoch))
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp syncservice.RESTItem
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if resp.DeletedAt == nil {
			t.Error("Expected deletedAt to be populated")
		}
	})
}

// TestMutationOnTombstone tests that mutation handlers return 410 for deleted items
func TestMutationOnTombstone(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	srv := &Server{
		DB:              pool,
		RateLimitConfig: DefaultRateLimitConfig,
		NoteSvc:         syncservice.NewNoteService(pool),
	}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	ctx := context.Background()
	userID := createTestUser(t, pool, testUserSubject) // Create user and get UUID

	// Create a session
	session := createTestSession(t, router)

	// Create and delete a note
	noteUID := uuid.New()
	_, err := srv.NoteSvc.ApplyNoteMutation(ctx, userID, map[string]any{
		"uid":     noteUID.String(),
		"title":   "Test",
		"content": "Original",
	}, syncservice.MutationOpts{})
	if err != nil {
		t.Fatalf("Failed to create note: %v", err)
	}

	_, err = srv.NoteSvc.ApplyNoteMutation(ctx, userID, map[string]any{
		"uid": noteUID.String(),
	}, syncservice.MutationOpts{SetDeleted: true})
	if err != nil {
		t.Fatalf("Failed to delete note: %v", err)
	}

	// Try to PATCH the tombstone (should get 410)
	t.Run("PATCH_returns_410_for_tombstone", func(t *testing.T) {
		patchPayload := map[string]any{"content": "Updated"}
		body := toJSONReader(patchPayload)

		req := httptest.NewRequest("PATCH", fmt.Sprintf("/v1/notes/%s", noteUID), body)
		req.Header.Set("X-Debug-Sub", testUserSubject)
		req.Header.Set("X-Sync-Session", session.ID)
		req.Header.Set("X-Sync-Epoch", fmt.Sprintf("%d", session.Epoch))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusGone {
			t.Errorf("Expected 410 Gone for PATCH on tombstone, got %d: %s", w.Code, w.Body.String())
		}
	})

	// Try to DELETE the tombstone again (should get 410)
	t.Run("DELETE_returns_410_for_tombstone", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", fmt.Sprintf("/v1/notes/%s", noteUID), nil)
		req.Header.Set("X-Debug-Sub", testUserSubject)
		req.Header.Set("X-Sync-Session", session.ID)
		req.Header.Set("X-Sync-Epoch", fmt.Sprintf("%d", session.Epoch))
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusGone {
			t.Errorf("Expected 410 Gone for DELETE on tombstone, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// toJSONReader converts a payload to a JSON reader for HTTP request bodies
func toJSONReader(payload interface{}) *bytes.Reader {
	data, _ := json.Marshal(payload)
	return bytes.NewReader(data)
}

// TestParseIfMatchHeader tests the ETag parsing from If-Match header
func TestParseIfMatchHeader(t *testing.T) {
	tests := []struct {
		name          string
		ifMatchValue  string
		wantVersion   int
		wantOk        bool
		description   string
	}{
		{
			name:         "quoted_etag_rfc_compliant",
			ifMatchValue: `"5"`,
			wantVersion:  5,
			wantOk:       true,
			description:  "RFC 7232 compliant quoted ETag should work",
		},
		{
			name:         "unquoted_etag",
			ifMatchValue: "5",
			wantVersion:  5,
			wantOk:       true,
			description:  "Unquoted ETag should also work for backward compatibility",
		},
		{
			name:         "quoted_zero",
			ifMatchValue: `"0"`,
			wantVersion:  0,
			wantOk:       true,
			description:  "Quoted zero should parse correctly",
		},
		{
			name:         "empty_header",
			ifMatchValue: "",
			wantVersion:  0,
			wantOk:       false,
			description:  "Empty header should return false",
		},
		{
			name:         "invalid_number",
			ifMatchValue: `"abc"`,
			wantVersion:  0,
			wantOk:       false,
			description:  "Non-numeric ETag should return false",
		},
		{
			name:         "quoted_large_version",
			ifMatchValue: `"12345"`,
			wantVersion:  12345,
			wantOk:       true,
			description:  "Quoted large version number should work",
		},
		{
			name:         "partially_quoted",
			ifMatchValue: `"5`,
			wantVersion:  0,
			wantOk:       false,
			description:  "Partially quoted value should fail",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.ifMatchValue != "" {
				req.Header.Set("If-Match", tt.ifMatchValue)
			}

			gotVersion, gotOk := parseIfMatchHeader(req)

			if gotVersion != tt.wantVersion {
				t.Errorf("%s: parseIfMatchHeader() version = %v, want %v", tt.description, gotVersion, tt.wantVersion)
			}
			if gotOk != tt.wantOk {
				t.Errorf("%s: parseIfMatchHeader() ok = %v, want %v", tt.description, gotOk, tt.wantOk)
			}
		})
	}
}

// TestOptimisticLocking_QuotedETag tests that optimistic locking works with quoted ETags
func TestOptimisticLocking_QuotedETag(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	srv := &Server{
		DB:              pool,
		RateLimitConfig: DefaultRateLimitConfig,
		NoteSvc:         syncservice.NewNoteService(pool),
	}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	ctx := context.Background()
	userID := createTestUser(t, pool, testUserSubject) // Create user and get UUID

	// Create a session
	session := createTestSession(t, router)

	// Create a note
	noteUID := uuid.New()
	notePayload := map[string]any{
		"uid":     noteUID.String(),
		"title":   "Original Title",
		"content": "Original content",
	}

	item, err := srv.NoteSvc.ApplyNoteMutation(ctx, userID, notePayload, syncservice.MutationOpts{})
	if err != nil {
		t.Fatalf("Failed to create note: %v", err)
	}

	currentVersion := item.Version

	t.Run("quoted_etag_allows_update_with_correct_version", func(t *testing.T) {
		updatePayload := map[string]any{"title": "Updated Title"}
		body := toJSONReader(updatePayload)

		req := httptest.NewRequest("PATCH", fmt.Sprintf("/v1/notes/%s", noteUID), body)
		req.Header.Set("X-Debug-Sub", testUserSubject)
		req.Header.Set("X-Sync-Session", session.ID)
		req.Header.Set("X-Sync-Epoch", fmt.Sprintf("%d", session.Epoch))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", fmt.Sprintf(`"%d"`, currentVersion)) // Quoted ETag
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK with quoted ETag, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("quoted_etag_rejects_stale_version", func(t *testing.T) {
		updatePayload := map[string]any{"title": "Stale Update"}
		body := toJSONReader(updatePayload)

		req := httptest.NewRequest("PATCH", fmt.Sprintf("/v1/notes/%s", noteUID), body)
		req.Header.Set("X-Debug-Sub", testUserSubject)
		req.Header.Set("X-Sync-Session", session.ID)
		req.Header.Set("X-Sync-Epoch", fmt.Sprintf("%d", session.Epoch))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", `"1"`) // Stale quoted ETag
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusPreconditionFailed {
			t.Errorf("Expected 412 Precondition Failed with stale quoted ETag, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("unquoted_etag_still_works", func(t *testing.T) {
		// Refresh current version
		currentItem, err := srv.NoteSvc.GetNote(ctx, userID, noteUID)
		if err != nil {
			t.Fatalf("Failed to get note: %v", err)
		}

		updatePayload := map[string]any{"title": "Another Update"}
		body := toJSONReader(updatePayload)

		req := httptest.NewRequest("PATCH", fmt.Sprintf("/v1/notes/%s", noteUID), body)
		req.Header.Set("X-Debug-Sub", testUserSubject)
		req.Header.Set("X-Sync-Session", session.ID)
		req.Header.Set("X-Sync-Epoch", fmt.Sprintf("%d", session.Epoch))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", fmt.Sprintf("%d", currentItem.Version)) // Unquoted ETag
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK with unquoted ETag, got %d: %s", w.Code, w.Body.String())
		}
	})
}
