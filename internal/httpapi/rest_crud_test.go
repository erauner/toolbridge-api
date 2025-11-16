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
	"github.com/google/uuid"
)

// TestNotesCRUD tests comprehensive CRUD operations for notes (mirrors bash script)
func TestNotesCRUD(t *testing.T) {
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
	userID := createTestUser(t, pool, testUserSubject)
	session := createTestSession(t, router)

	tests := []struct {
		name           string
		method         string
		pathFunc       func(noteUID string) string
		body           map[string]any
		setupFunc      func() string // Returns note UID for test
		expectedStatus int
		checkResponse  func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name:   "POST - Create note",
			method: "POST",
			pathFunc: func(_ string) string {
				return "/v1/notes"
			},
			body: map[string]any{
				"title":   "Test Note",
				"content": "REST API Testing",
			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTItem
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if resp.Version != 1 {
					t.Errorf("Expected version 1, got %d", resp.Version)
				}
				if resp.UID == "" {
					t.Error("Expected UID to be set")
				}
			},
		},
		{
			name:   "GET - Retrieve note",
			method: "GET",
			pathFunc: func(noteUID string) string {
				return fmt.Sprintf("/v1/notes/%s", noteUID)
			},
			setupFunc: func() string {
				noteUID := uuid.New()
				_, err := srv.NoteSvc.ApplyNoteMutation(ctx, userID, map[string]any{
					"uid":     noteUID.String(),
					"title":   "Get Test Note",
					"content": "Testing GET",
				}, syncservice.MutationOpts{})
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
				return noteUID.String()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTItem
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if resp.UID == "" {
					t.Error("Expected UID to be set")
				}
			},
		},
		{
			name:   "PATCH - Update note with If-Match",
			method: "PATCH",
			pathFunc: func(noteUID string) string {
				return fmt.Sprintf("/v1/notes/%s", noteUID)
			},
			body: map[string]any{
				"content": "Updated content",
			},
			setupFunc: func() string {
				noteUID := uuid.New()
				_, err := srv.NoteSvc.ApplyNoteMutation(ctx, userID, map[string]any{
					"uid":     noteUID.String(),
					"title":   "Patch Test Note",
					"content": "Original",
				}, syncservice.MutationOpts{})
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
				return noteUID.String()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTItem
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if resp.Version != 2 {
					t.Errorf("Expected version 2 after update, got %d", resp.Version)
				}
			},
		},
		{
			name:   "DELETE - Soft delete note",
			method: "DELETE",
			pathFunc: func(noteUID string) string {
				return fmt.Sprintf("/v1/notes/%s", noteUID)
			},
			setupFunc: func() string {
				noteUID := uuid.New()
				_, err := srv.NoteSvc.ApplyNoteMutation(ctx, userID, map[string]any{
					"uid":     noteUID.String(),
					"title":   "Delete Test Note",
					"content": "Will be deleted",
				}, syncservice.MutationOpts{})
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
				return noteUID.String()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTItem
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if resp.DeletedAt == nil {
					t.Error("Expected deletedAt to be set")
				}
			},
		},
		{
			name:   "GET - 410 Gone for deleted note",
			method: "GET",
			pathFunc: func(noteUID string) string {
				return fmt.Sprintf("/v1/notes/%s", noteUID)
			},
			setupFunc: func() string {
				noteUID := uuid.New()
				_, err := srv.NoteSvc.ApplyNoteMutation(ctx, userID, map[string]any{
					"uid":   noteUID.String(),
					"title": "Deleted Note",
				}, syncservice.MutationOpts{})
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
				// Delete it
				_, err = srv.NoteSvc.ApplyNoteMutation(ctx, userID, map[string]any{
					"uid": noteUID.String(),
				}, syncservice.MutationOpts{SetDeleted: true})
				if err != nil {
					t.Fatalf("Delete failed: %v", err)
				}
				return noteUID.String()
			},
			expectedStatus: http.StatusGone,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp map[string]any
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if resp["error"] != "note deleted" {
					t.Errorf("Expected error 'note deleted', got %v", resp["error"])
				}
			},
		},
		{
			name:   "GET - 200 for deleted note with includeDeleted=true",
			method: "GET",
			pathFunc: func(noteUID string) string {
				return fmt.Sprintf("/v1/notes/%s?includeDeleted=true", noteUID)
			},
			setupFunc: func() string {
				noteUID := uuid.New()
				_, err := srv.NoteSvc.ApplyNoteMutation(ctx, userID, map[string]any{
					"uid":   noteUID.String(),
					"title": "Tombstone Note",
				}, syncservice.MutationOpts{})
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
				// Delete it
				_, err = srv.NoteSvc.ApplyNoteMutation(ctx, userID, map[string]any{
					"uid": noteUID.String(),
				}, syncservice.MutationOpts{SetDeleted: true})
				if err != nil {
					t.Fatalf("Delete failed: %v", err)
				}
				return noteUID.String()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTItem
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if resp.DeletedAt == nil {
					t.Error("Expected deletedAt to be set on tombstone")
				}
			},
		},
		{
			name:   "PATCH - 410 for mutation on deleted note (no resurrection)",
			method: "PATCH",
			pathFunc: func(noteUID string) string {
				return fmt.Sprintf("/v1/notes/%s", noteUID)
			},
			body: map[string]any{
				"content": "Resurrection attempt",
			},
			setupFunc: func() string {
				noteUID := uuid.New()
				_, err := srv.NoteSvc.ApplyNoteMutation(ctx, userID, map[string]any{
					"uid":   noteUID.String(),
					"title": "No Resurrection",
				}, syncservice.MutationOpts{})
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
				// Delete it
				_, err = srv.NoteSvc.ApplyNoteMutation(ctx, userID, map[string]any{
					"uid": noteUID.String(),
				}, syncservice.MutationOpts{SetDeleted: true})
				if err != nil {
					t.Fatalf("Delete failed: %v", err)
				}
				return noteUID.String()
			},
			expectedStatus: http.StatusGone,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp map[string]any
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if resp["error"] != "note deleted" {
					t.Errorf("Expected error 'note deleted', got %v", resp["error"])
				}
			},
		},
		{
			name:   "GET - List notes",
			method: "GET",
			pathFunc: func(_ string) string {
				return "/v1/notes"
			},
			setupFunc: func() string {
				// Create a few notes for listing
				for i := 0; i < 3; i++ {
					noteUID := uuid.New()
					_, err := srv.NoteSvc.ApplyNoteMutation(ctx, userID, map[string]any{
						"uid":     noteUID.String(),
						"title":   fmt.Sprintf("List Test Note %d", i),
						"content": "For listing",
					}, syncservice.MutationOpts{})
					if err != nil {
						t.Fatalf("Setup failed: %v", err)
					}
				}
				return "" // Not using a specific UID for list
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTListResponse
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if len(resp.Items) == 0 {
					t.Error("Expected at least one note in list")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var noteUID string
			if tt.setupFunc != nil {
				noteUID = tt.setupFunc()
			}

			path := tt.pathFunc(noteUID)
			w := makeRequestWithSession(t, router, tt.method, path, tt.body, session)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}

			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// TestTasksCRUD tests comprehensive CRUD operations for tasks
func TestTasksCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	srv := &Server{
		DB:              pool,
		RateLimitConfig: DefaultRateLimitConfig,
		TaskSvc:         syncservice.NewTaskService(pool),
	}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	ctx := context.Background()
	userID := createTestUser(t, pool, testUserSubject)
	session := createTestSession(t, router)

	tests := []struct {
		name           string
		method         string
		pathFunc       func(taskUID string) string
		body           map[string]any
		setupFunc      func() string
		expectedStatus int
		checkResponse  func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name:   "POST - Create task",
			method: "POST",
			pathFunc: func(_ string) string {
				return "/v1/tasks"
			},
			body: map[string]any{
				"title": "Test Task",
				"done":  false,
			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTItem
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if resp.Version != 1 {
					t.Errorf("Expected version 1, got %d", resp.Version)
				}
			},
		},
		{
			name:   "GET - Retrieve task",
			method: "GET",
			pathFunc: func(taskUID string) string {
				return fmt.Sprintf("/v1/tasks/%s", taskUID)
			},
			setupFunc: func() string {
				taskUID := uuid.New()
				_, err := srv.TaskSvc.ApplyTaskMutation(ctx, userID, map[string]any{
					"uid":   taskUID.String(),
					"title": "Get Test Task",
					"done":  false,
				}, syncservice.MutationOpts{})
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
				return taskUID.String()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:   "PATCH - Update task (mark as done)",
			method: "PATCH",
			pathFunc: func(taskUID string) string {
				return fmt.Sprintf("/v1/tasks/%s", taskUID)
			},
			body: map[string]any{
				"done": true,
			},
			setupFunc: func() string {
				taskUID := uuid.New()
				_, err := srv.TaskSvc.ApplyTaskMutation(ctx, userID, map[string]any{
					"uid":   taskUID.String(),
					"title": "Patch Test Task",
					"done":  false,
				}, syncservice.MutationOpts{})
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
				return taskUID.String()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTItem
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if resp.Version != 2 {
					t.Errorf("Expected version 2 after update, got %d", resp.Version)
				}
			},
		},
		{
			name:   "GET - List tasks",
			method: "GET",
			pathFunc: func(_ string) string {
				return "/v1/tasks"
			},
			setupFunc: func() string {
				// Create a few tasks for listing
				for i := 0; i < 2; i++ {
					taskUID := uuid.New()
					_, err := srv.TaskSvc.ApplyTaskMutation(ctx, userID, map[string]any{
						"uid":   taskUID.String(),
						"title": fmt.Sprintf("List Task %d", i),
						"done":  false,
					}, syncservice.MutationOpts{})
					if err != nil {
						t.Fatalf("Setup failed: %v", err)
					}
				}
				return ""
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTListResponse
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if len(resp.Items) == 0 {
					t.Error("Expected at least one task in list")
				}
			},
		},
		{
			name:   "DELETE - Soft delete task",
			method: "DELETE",
			pathFunc: func(taskUID string) string {
				return fmt.Sprintf("/v1/tasks/%s", taskUID)
			},
			setupFunc: func() string {
				taskUID := uuid.New()
				_, err := srv.TaskSvc.ApplyTaskMutation(ctx, userID, map[string]any{
					"uid":   taskUID.String(),
					"title": "Delete Test Task",
					"done":  false,
				}, syncservice.MutationOpts{})
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
				return taskUID.String()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTItem
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if resp.DeletedAt == nil {
					t.Error("Expected deletedAt to be set")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var taskUID string
			if tt.setupFunc != nil {
				taskUID = tt.setupFunc()
			}

			path := tt.pathFunc(taskUID)
			w := makeRequestWithSession(t, router, tt.method, path, tt.body, session)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}

			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// TestOptimisticLocking tests version conflict detection across entities
func TestOptimisticLocking(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	srv := &Server{
		DB:              pool,
		RateLimitConfig: DefaultRateLimitConfig,
		NoteSvc:         syncservice.NewNoteService(pool),
		TaskSvc:         syncservice.NewTaskService(pool),
	}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	ctx := context.Background()
	userID := createTestUser(t, pool, testUserSubject)
	session := createTestSession(t, router)

	tests := []struct {
		name       string
		entityType string
		setupFunc  func() (string, int) // Returns UID and version
		updateBody map[string]any
		staleETag  string // Intentionally stale version
	}{
		{
			name:       "Note - Reject stale version",
			entityType: "notes",
			setupFunc: func() (string, int) {
				noteUID := uuid.New()
				item, err := srv.NoteSvc.ApplyNoteMutation(ctx, userID, map[string]any{
					"uid":     noteUID.String(),
					"title":   "Locking Test",
					"content": "Version 1",
				}, syncservice.MutationOpts{})
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
				// Make another update to bump version to 2
				item, err = srv.NoteSvc.ApplyNoteMutation(ctx, userID, map[string]any{
					"uid":     noteUID.String(),
					"content": "Version 2",
				}, syncservice.MutationOpts{
					EnforceVersion:  true,
					ExpectedVersion: item.Version,
				})
				if err != nil {
					t.Fatalf("Second update failed: %v", err)
				}
				return noteUID.String(), item.Version
			},
			updateBody: map[string]any{
				"content": "This should fail",
			},
			staleETag: "1", // Version is actually 2
		},
		{
			name:       "Task - Reject stale version",
			entityType: "tasks",
			setupFunc: func() (string, int) {
				taskUID := uuid.New()
				item, err := srv.TaskSvc.ApplyTaskMutation(ctx, userID, map[string]any{
					"uid":   taskUID.String(),
					"title": "Locking Test Task",
					"done":  false,
				}, syncservice.MutationOpts{})
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
				// Make another update to bump version
				item, err = srv.TaskSvc.ApplyTaskMutation(ctx, userID, map[string]any{
					"uid":  taskUID.String(),
					"done": true,
				}, syncservice.MutationOpts{
					EnforceVersion:  true,
					ExpectedVersion: item.Version,
				})
				if err != nil {
					t.Fatalf("Second update failed: %v", err)
				}
				return taskUID.String(), item.Version
			},
			updateBody: map[string]any{
				"title": "This should fail",
			},
			staleETag: "1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entityUID, currentVersion := tt.setupFunc()

			// Try to update with stale version (should get 409)
			// Note: We cannot easily test If-Match header with makeRequestWithSession helper
			// So we'll use a direct HTTP request here
			path := fmt.Sprintf("/v1/%s/%s", tt.entityType, entityUID)
			bodyBytes, _ := json.Marshal(tt.updateBody)
			req := httptest.NewRequest("PATCH", path, bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Debug-Sub", testUserSubject)
			req.Header.Set("X-Sync-Session", session.ID)
			req.Header.Set("X-Sync-Epoch", fmt.Sprintf("%d", session.Epoch))
			req.Header.Set("If-Match", fmt.Sprintf("\"%s\"", tt.staleETag)) // Quoted ETag

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// Should get 412 Precondition Failed when using If-Match with stale version (RFC 7232)
			if w.Code != http.StatusPreconditionFailed {
				t.Errorf("Expected 412 Precondition Failed for stale If-Match, got %d. Body: %s", w.Code, w.Body.String())
			}

			// Verify current version is still intact
			getPath := fmt.Sprintf("/v1/%s/%s", tt.entityType, entityUID)
			getW := makeRequestWithSession(t, router, "GET", getPath, nil, session)

			if getW.Code != http.StatusOK {
				t.Fatalf("Failed to verify entity after conflict: %d", getW.Code)
			}

			var item syncservice.RESTItem
			if err := json.NewDecoder(getW.Body).Decode(&item); err != nil {
				t.Fatalf("Failed to decode: %v", err)
			}

			if item.Version != currentVersion {
				t.Errorf("Expected version %d to be unchanged, got %d", currentVersion, item.Version)
			}
		})
	}
}

// TestChatsCRUD tests comprehensive CRUD operations for chats
func TestChatsCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	srv := &Server{
		DB:              pool,
		RateLimitConfig: DefaultRateLimitConfig,
		ChatSvc:         syncservice.NewChatService(pool),
	}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	ctx := context.Background()
	userID := createTestUser(t, pool, testUserSubject)
	session := createTestSession(t, router)

	tests := []struct {
		name           string
		method         string
		pathFunc       func(chatUID string) string
		body           map[string]any
		setupFunc      func() string
		expectedStatus int
		checkResponse  func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name:   "POST - Create chat",
			method: "POST",
			pathFunc: func(_ string) string {
				return "/v1/chats"
			},
			body: map[string]any{
				"title": "Test Chat",
			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTItem
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if resp.Version != 1 {
					t.Errorf("Expected version 1, got %d", resp.Version)
				}
			},
		},
		{
			name:   "GET - Retrieve chat",
			method: "GET",
			pathFunc: func(chatUID string) string {
				return fmt.Sprintf("/v1/chats/%s", chatUID)
			},
			setupFunc: func() string {
				chatUID := uuid.New()
				_, err := srv.ChatSvc.ApplyChatMutation(ctx, userID, map[string]any{
					"uid":   chatUID.String(),
					"title": "Get Test Chat",
				}, syncservice.MutationOpts{})
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
				return chatUID.String()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:   "PATCH - Update chat",
			method: "PATCH",
			pathFunc: func(chatUID string) string {
				return fmt.Sprintf("/v1/chats/%s", chatUID)
			},
			body: map[string]any{
				"title": "Updated Chat",
			},
			setupFunc: func() string {
				chatUID := uuid.New()
				_, err := srv.ChatSvc.ApplyChatMutation(ctx, userID, map[string]any{
					"uid":   chatUID.String(),
					"title": "Original Chat",
				}, syncservice.MutationOpts{})
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
				return chatUID.String()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTItem
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if resp.Version != 2 {
					t.Errorf("Expected version 2 after update, got %d", resp.Version)
				}
			},
		},
		{
			name:   "GET - List chats",
			method: "GET",
			pathFunc: func(_ string) string {
				return "/v1/chats"
			},
			setupFunc: func() string {
				// Create a few chats for listing
				for i := 0; i < 2; i++ {
					chatUID := uuid.New()
					_, err := srv.ChatSvc.ApplyChatMutation(ctx, userID, map[string]any{
						"uid":   chatUID.String(),
						"title": fmt.Sprintf("List Chat %d", i),
					}, syncservice.MutationOpts{})
					if err != nil {
						t.Fatalf("Setup failed: %v", err)
					}
				}
				return ""
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTListResponse
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if len(resp.Items) == 0 {
					t.Error("Expected at least one chat in list")
				}
			},
		},
		{
			name:   "DELETE - Soft delete chat",
			method: "DELETE",
			pathFunc: func(chatUID string) string {
				return fmt.Sprintf("/v1/chats/%s", chatUID)
			},
			setupFunc: func() string {
				chatUID := uuid.New()
				_, err := srv.ChatSvc.ApplyChatMutation(ctx, userID, map[string]any{
					"uid":   chatUID.String(),
					"title": "Delete Test Chat",
				}, syncservice.MutationOpts{})
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
				return chatUID.String()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTItem
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if resp.DeletedAt == nil {
					t.Error("Expected deletedAt to be set")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var chatUID string
			if tt.setupFunc != nil {
				chatUID = tt.setupFunc()
			}

			path := tt.pathFunc(chatUID)
			w := makeRequestWithSession(t, router, tt.method, path, tt.body, session)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}

			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// TestChatMessagesCRUD tests CRUD operations for chat messages with parent validation
func TestChatMessagesCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	srv := &Server{
		DB:              pool,
		RateLimitConfig: DefaultRateLimitConfig,
		ChatSvc:         syncservice.NewChatService(pool),
		ChatMessageSvc:  syncservice.NewChatMessageService(pool),
	}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	ctx := context.Background()
	userID := createTestUser(t, pool, testUserSubject)
	session := createTestSession(t, router)

	// Create a parent chat for all message tests
	chatUID := uuid.New()
	_, err := srv.ChatSvc.ApplyChatMutation(ctx, userID, map[string]any{
		"uid":   chatUID.String(),
		"title": "Parent Chat for Messages",
	}, syncservice.MutationOpts{})
	if err != nil {
		t.Fatalf("Failed to create parent chat: %v", err)
	}

	tests := []struct {
		name           string
		method         string
		pathFunc       func(msgUID string) string
		body           map[string]any
		setupFunc      func() string
		expectedStatus int
		checkResponse  func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name:   "POST - Create message in chat",
			method: "POST",
			pathFunc: func(_ string) string {
				return "/v1/chat_messages"
			},
			body: map[string]any{
				"chatUid": chatUID.String(),
				"role":    "user",
				"content": "Hello!",
			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTItem
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if resp.Version != 1 {
					t.Errorf("Expected version 1, got %d", resp.Version)
				}
			},
		},
		{
			name:   "GET - Retrieve message",
			method: "GET",
			pathFunc: func(msgUID string) string {
				return fmt.Sprintf("/v1/chat_messages/%s", msgUID)
			},
			setupFunc: func() string {
				msgUID := uuid.New()
				_, err := srv.ChatMessageSvc.ApplyChatMessageMutation(ctx, userID, map[string]any{
					"uid":     msgUID.String(),
					"chatUid": chatUID.String(),
					"role":    "user",
					"content": "Test message",
				}, syncservice.MutationOpts{})
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
				return msgUID.String()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:   "PATCH - Update message",
			method: "PATCH",
			pathFunc: func(msgUID string) string {
				return fmt.Sprintf("/v1/chat_messages/%s", msgUID)
			},
			body: map[string]any{
				"content": "Updated message",
			},
			setupFunc: func() string {
				msgUID := uuid.New()
				_, err := srv.ChatMessageSvc.ApplyChatMessageMutation(ctx, userID, map[string]any{
					"uid":     msgUID.String(),
					"chatUid": chatUID.String(),
					"role":    "user",
					"content": "Original message",
				}, syncservice.MutationOpts{})
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
				return msgUID.String()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTItem
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if resp.Version != 2 {
					t.Errorf("Expected version 2 after update, got %d", resp.Version)
				}
			},
		},
		{
			name:   "GET - List messages",
			method: "GET",
			pathFunc: func(_ string) string {
				return "/v1/chat_messages"
			},
			setupFunc: func() string {
				// Create a few messages for listing
				for i := 0; i < 2; i++ {
					msgUID := uuid.New()
					_, err := srv.ChatMessageSvc.ApplyChatMessageMutation(ctx, userID, map[string]any{
						"uid":     msgUID.String(),
						"chatUid": chatUID.String(),
						"role":    "user",
						"content": fmt.Sprintf("List Message %d", i),
					}, syncservice.MutationOpts{})
					if err != nil {
						t.Fatalf("Setup failed: %v", err)
					}
				}
				return ""
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTListResponse
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if len(resp.Items) == 0 {
					t.Error("Expected at least one message in list")
				}
			},
		},
		{
			name:   "DELETE - Soft delete message",
			method: "DELETE",
			pathFunc: func(msgUID string) string {
				return fmt.Sprintf("/v1/chat_messages/%s", msgUID)
			},
			setupFunc: func() string {
				msgUID := uuid.New()
				_, err := srv.ChatMessageSvc.ApplyChatMessageMutation(ctx, userID, map[string]any{
					"uid":     msgUID.String(),
					"chatUid": chatUID.String(),
					"role":    "user",
					"content": "To be deleted",
				}, syncservice.MutationOpts{})
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
				return msgUID.String()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTItem
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if resp.DeletedAt == nil {
					t.Error("Expected deletedAt to be set")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var msgUID string
			if tt.setupFunc != nil {
				msgUID = tt.setupFunc()
			}

			path := tt.pathFunc(msgUID)
			w := makeRequestWithSession(t, router, tt.method, path, tt.body, session)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}

			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// TestCommentsCRUD tests CRUD operations for comments with parent validation
func TestCommentsCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	srv := &Server{
		DB:              pool,
		RateLimitConfig: DefaultRateLimitConfig,
		NoteSvc:         syncservice.NewNoteService(pool),
		CommentSvc:      syncservice.NewCommentService(pool),
	}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	ctx := context.Background()
	userID := createTestUser(t, pool, testUserSubject)
	session := createTestSession(t, router)

	// Create a parent note for all comment tests
	noteUID := uuid.New()
	_, err := srv.NoteSvc.ApplyNoteMutation(ctx, userID, map[string]any{
		"uid":     noteUID.String(),
		"title":   "Parent Note for Comments",
		"content": "Test note",
	}, syncservice.MutationOpts{})
	if err != nil {
		t.Fatalf("Failed to create parent note: %v", err)
	}

	tests := []struct {
		name           string
		method         string
		pathFunc       func(commentUID string) string
		body           map[string]any
		setupFunc      func() string
		expectedStatus int
		checkResponse  func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name:   "POST - Create comment on note",
			method: "POST",
			pathFunc: func(_ string) string {
				return "/v1/comments"
			},
			body: map[string]any{
				"parentType": "note",
				"parentUid":  noteUID.String(),
				"content":    "Great note!",
			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTItem
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if resp.Version != 1 {
					t.Errorf("Expected version 1, got %d", resp.Version)
				}
			},
		},
		{
			name:   "GET - Retrieve comment",
			method: "GET",
			pathFunc: func(commentUID string) string {
				return fmt.Sprintf("/v1/comments/%s", commentUID)
			},
			setupFunc: func() string {
				commentUID := uuid.New()
				_, err := srv.CommentSvc.ApplyCommentMutation(ctx, userID, map[string]any{
					"uid":        commentUID.String(),
					"parentType": "note",
					"parentUid":  noteUID.String(),
					"content":    "Test comment",
				}, syncservice.MutationOpts{})
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
				return commentUID.String()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:   "PATCH - Update comment",
			method: "PATCH",
			pathFunc: func(commentUID string) string {
				return fmt.Sprintf("/v1/comments/%s", commentUID)
			},
			body: map[string]any{
				"content": "Updated comment",
			},
			setupFunc: func() string {
				commentUID := uuid.New()
				_, err := srv.CommentSvc.ApplyCommentMutation(ctx, userID, map[string]any{
					"uid":        commentUID.String(),
					"parentType": "note",
					"parentUid":  noteUID.String(),
					"content":    "Original comment",
				}, syncservice.MutationOpts{})
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
				return commentUID.String()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTItem
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if resp.Version != 2 {
					t.Errorf("Expected version 2 after update, got %d", resp.Version)
				}
			},
		},
		{
			name:   "GET - List comments",
			method: "GET",
			pathFunc: func(_ string) string {
				return "/v1/comments"
			},
			setupFunc: func() string {
				// Create a few comments for listing
				for i := 0; i < 2; i++ {
					commentUID := uuid.New()
					_, err := srv.CommentSvc.ApplyCommentMutation(ctx, userID, map[string]any{
						"uid":        commentUID.String(),
						"parentType": "note",
						"parentUid":  noteUID.String(),
						"content":    fmt.Sprintf("List Comment %d", i),
					}, syncservice.MutationOpts{})
					if err != nil {
						t.Fatalf("Setup failed: %v", err)
					}
				}
				return ""
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTListResponse
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if len(resp.Items) == 0 {
					t.Error("Expected at least one comment in list")
				}
			},
		},
		{
			name:   "DELETE - Soft delete comment",
			method: "DELETE",
			pathFunc: func(commentUID string) string {
				return fmt.Sprintf("/v1/comments/%s", commentUID)
			},
			setupFunc: func() string {
				commentUID := uuid.New()
				_, err := srv.CommentSvc.ApplyCommentMutation(ctx, userID, map[string]any{
					"uid":        commentUID.String(),
					"parentType": "note",
					"parentUid":  noteUID.String(),
					"content":    "To be deleted",
				}, syncservice.MutationOpts{})
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
				return commentUID.String()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp syncservice.RESTItem
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if resp.DeletedAt == nil {
					t.Error("Expected deletedAt to be set")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var commentUID string
			if tt.setupFunc != nil {
				commentUID = tt.setupFunc()
			}

			path := tt.pathFunc(commentUID)
			w := makeRequestWithSession(t, router, tt.method, path, tt.body, session)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}

			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}
