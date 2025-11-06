package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/erauner12/toolbridge-api/internal/auth"
)

// setupCommentTest creates a note and task for testing comments
func setupCommentTest(t *testing.T, router http.Handler, sessionID string) (noteUID, taskUID string) {
	t.Helper()

	// Create a note
	noteUID = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	makeRequestWithSession(t, router, "POST", "/v1/sync/notes/push", pushReq{
		Items: []map[string]any{
			{
				"uid":       noteUID,
				"title":     "Test Note for Comments",
				"updatedTs": "2025-11-03T10:00:00Z",
				"sync":      map[string]any{"version": float64(1)},
			},
		},
	}, sessionID)

	// Create a task
	taskUID = "b2c3d4e5-f6a7-8901-bcde-f12345678901"
	makeRequestWithSession(t, router, "POST", "/v1/sync/tasks/push", pushReq{
		Items: []map[string]any{
			{
				"uid":       taskUID,
				"title":     "Test Task for Comments",
				"updatedTs": "2025-11-03T10:00:00Z",
				"sync":      map[string]any{"version": float64(1)},
			},
		},
	}, sessionID)

	return noteUID, taskUID
}

func TestPushComments_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	// Clean up tables before test
	_, _ = pool.Exec(context.Background(), "DELETE FROM comment")
	_, _ = pool.Exec(context.Background(), "DELETE FROM task")
	_, _ = pool.Exec(context.Background(), "DELETE FROM note")

	srv := &Server{DB: pool}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Create a session for this test suite
	sessionID := createTestSession(t, router)

	// Setup parent entities
	noteUID, taskUID := setupCommentTest(t, router, sessionID)

	tests := []struct {
		name       string
		body       pushReq
		wantStatus int
		checkResp  func(*testing.T, []pushAck)
	}{
		{
			name: "push comment for note",
			body: pushReq{
				Items: []map[string]any{
					{
						"uid":        "c1d2e3f4-a1b2-3c4d-5e6f-7a8b9c0d1e2f",
						"content":    "Test comment on note",
						"parentType": "note",
						"parentUid":  noteUID,
						"updatedTs":  "2025-11-03T10:00:00Z",
						"sync": map[string]any{
							"version":   float64(1),
							"isDeleted": false,
						},
					},
				},
			},
			wantStatus: 200,
			checkResp: func(t *testing.T, acks []pushAck) {
				if len(acks) != 1 {
					t.Fatalf("Expected 1 ack, got %d", len(acks))
				}
				if acks[0].Error != "" {
					t.Errorf("Expected no error, got: %s", acks[0].Error)
				}
				if acks[0].Version != 1 {
					t.Errorf("Expected version 1, got %d", acks[0].Version)
				}
			},
		},
		{
			name: "push comment for task",
			body: pushReq{
				Items: []map[string]any{
					{
						"uid":        "d2e3f4a5-b2c3-4d5e-6f7a-8b9c0d1e2f3a",
						"content":    "Test comment on task",
						"parentType": "task",
						"parentUid":  taskUID,
						"updatedTs":  "2025-11-03T10:00:00Z",
						"sync": map[string]any{
							"version":   float64(1),
							"isDeleted": false,
						},
					},
				},
			},
			wantStatus: 200,
			checkResp: func(t *testing.T, acks []pushAck) {
				if len(acks) != 1 {
					t.Fatalf("Expected 1 ack, got %d", len(acks))
				}
				if acks[0].Error != "" {
					t.Errorf("Expected no error, got: %s", acks[0].Error)
				}
				if acks[0].Version != 1 {
					t.Errorf("Expected version 1, got %d", acks[0].Version)
				}
			},
		},
		{
			name: "push duplicate (idempotency)",
			body: pushReq{
				Items: []map[string]any{
					{
						"uid":        "c1d2e3f4-a1b2-3c4d-5e6f-7a8b9c0d1e2f",
						"content":    "Test comment on note",
						"parentType": "note",
						"parentUid":  noteUID,
						"updatedTs":  "2025-11-03T10:00:00Z",
						"sync": map[string]any{
							"version": float64(1),
						},
					},
				},
			},
			wantStatus: 200,
			checkResp: func(t *testing.T, acks []pushAck) {
				// Version should stay at 1 (idempotent)
				if acks[0].Version != 1 {
					t.Errorf("Expected version 1 (no increment), got %d", acks[0].Version)
				}
			},
		},
		{
			name: "push newer timestamp (LWW)",
			body: pushReq{
				Items: []map[string]any{
					{
						"uid":        "c1d2e3f4-a1b2-3c4d-5e6f-7a8b9c0d1e2f",
						"content":    "Updated comment",
						"parentType": "note",
						"parentUid":  noteUID,
						"updatedTs":  "2025-11-03T10:01:00Z", // Newer timestamp
						"sync": map[string]any{
							"version": float64(1),
						},
					},
				},
			},
			wantStatus: 200,
			checkResp: func(t *testing.T, acks []pushAck) {
				// Version should increment to 2
				if acks[0].Version != 2 {
					t.Errorf("Expected version 2 (incremented), got %d", acks[0].Version)
				}
			},
		},
		{
			name: "push with missing parentUid",
			body: pushReq{
				Items: []map[string]any{
					{
						"uid":        "e3f4a5b6-c3d4-5e6f-7a8b-9c0d1e2f3a4b",
						"content":    "Comment without parent",
						"parentType": "note",
						"updatedTs":  "2025-11-03T10:00:00Z",
					},
				},
			},
			wantStatus: 200,
			checkResp: func(t *testing.T, acks []pushAck) {
				if acks[0].Error == "" {
					t.Error("Expected error for missing parentUid")
				}
			},
		},
		{
			name: "push with non-existent parent note",
			body: pushReq{
				Items: []map[string]any{
					{
						"uid":        "f4a5b6c7-d4e5-6f7a-8b9c-0d1e2f3a4b5c",
						"content":    "Comment on non-existent note",
						"parentType": "note",
						"parentUid":  "99999999-9999-9999-9999-999999999999",
						"updatedTs":  "2025-11-03T10:00:00Z",
						"sync": map[string]any{
							"version": float64(1),
						},
					},
				},
			},
			wantStatus: 200,
			checkResp: func(t *testing.T, acks []pushAck) {
				if acks[0].Error == "" {
					t.Error("Expected error for non-existent parent")
				}
				if acks[0].Error != "" && len(acks[0].Error) > 0 {
					// Should contain "parent" and "not found"
					t.Logf("Got expected error: %s", acks[0].Error)
				}
			},
		},
		{
			name: "push with non-existent parent task",
			body: pushReq{
				Items: []map[string]any{
					{
						"uid":        "a5b6c7d8-e5f6-7a8b-9c0d-1e2f3a4b5c6d",
						"content":    "Comment on non-existent task",
						"parentType": "task",
						"parentUid":  "88888888-8888-8888-8888-888888888888",
						"updatedTs":  "2025-11-03T10:00:00Z",
						"sync": map[string]any{
							"version": float64(1),
						},
					},
				},
			},
			wantStatus: 200,
			checkResp: func(t *testing.T, acks []pushAck) {
				if acks[0].Error == "" {
					t.Error("Expected error for non-existent parent")
				}
			},
		},
		{
			name: "push with invalid parent type",
			body: pushReq{
				Items: []map[string]any{
					{
						"uid":        "b6c7d8e9-f6a7-8b9c-0d1e-2f3a4b5c6d7e",
						"content":    "Comment with invalid parent type",
						"parentType": "invalid",
						"parentUid":  noteUID,
						"updatedTs":  "2025-11-03T10:00:00Z",
						"sync": map[string]any{
							"version": float64(1),
						},
					},
				},
			},
			wantStatus: 200,
			checkResp: func(t *testing.T, acks []pushAck) {
				if acks[0].Error == "" {
					t.Error("Expected error for invalid parent type")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := makeRequestWithSession(t, router, "POST", "/v1/sync/comments/push", tt.body, sessionID)

			if rec.Code != tt.wantStatus {
				t.Errorf("Status = %d, want %d", rec.Code, tt.wantStatus)
			}

			var acks []pushAck
			if err := json.NewDecoder(rec.Body).Decode(&acks); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if tt.checkResp != nil {
				tt.checkResp(t, acks)
			}
		})
	}
}

func TestPushCommentsOnDeletedParent_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	// Clean up tables before test
	_, _ = pool.Exec(context.Background(), "DELETE FROM comment")
	_, _ = pool.Exec(context.Background(), "DELETE FROM task")
	_, _ = pool.Exec(context.Background(), "DELETE FROM note")

	srv := &Server{DB: pool}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Create a session for this test suite
	sessionID := createTestSession(t, router)

	// Create a note
	noteUID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	makeRequestWithSession(t, router, "POST", "/v1/sync/notes/push", pushReq{
		Items: []map[string]any{
			{
				"uid":       noteUID,
				"title":     "Note to be deleted",
				"updatedTs": "2025-11-03T10:00:00Z",
				"sync":      map[string]any{"version": float64(1)},
			},
		},
	}, sessionID)

	// Soft delete the note
	makeRequestWithSession(t, router, "POST", "/v1/sync/notes/push", pushReq{
		Items: []map[string]any{
			{
				"uid":       noteUID,
				"title":     "Note to be deleted",
				"updatedTs": "2025-11-03T10:01:00Z",
				"sync": map[string]any{
					"version":   float64(1),
					"isDeleted": true,
					"deletedAt": "2025-11-03T10:01:00Z",
				},
			},
		},
	}, sessionID)

	// Try to create a comment on the soft-deleted note (should fail)
	commentRec := makeRequestWithSession(t, router, "POST", "/v1/sync/comments/push", pushReq{
		Items: []map[string]any{
			{
				"uid":        "c1d2e3f4-a1b2-3c4d-5e6f-7a8b9c0d1e2f",
				"content":    "Comment on deleted note",
				"parentType": "note",
				"parentUid":  noteUID,
				"updatedTs":  "2025-11-03T10:02:00Z",
				"sync": map[string]any{
					"version": float64(1),
				},
			},
		},
	}, sessionID)

	var acks []pushAck
	if err := json.NewDecoder(commentRec.Body).Decode(&acks); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Should get an error about parent not found
	if len(acks) != 1 {
		t.Fatalf("Expected 1 ack, got %d", len(acks))
	}
	if acks[0].Error == "" {
		t.Error("Expected error for comment on deleted parent, got none")
	}
	if acks[0].Error != "" {
		t.Logf("Got expected error: %s", acks[0].Error)
	}
}

func TestDeleteCommentAfterParentDeleted_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	// Clean up
	_, _ = pool.Exec(context.Background(), "DELETE FROM comment")
	_, _ = pool.Exec(context.Background(), "DELETE FROM task")
	_, _ = pool.Exec(context.Background(), "DELETE FROM note")

	srv := &Server{DB: pool}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Create a session for this test suite
	sessionID := createTestSession(t, router)

	// Create a note
	noteUID := "b2c3d4e5-f6a7-8901-bcde-f2345678901a"
	makeRequestWithSession(t, router, "POST", "/v1/sync/notes/push", pushReq{
		Items: []map[string]any{
			{
				"uid":       noteUID,
				"title":     "Note to be deleted",
				"content":   "This note will be deleted",
				"updatedTs": "2025-11-03T10:00:00Z",
				"sync":      map[string]any{"version": float64(1)},
			},
		},
	}, sessionID)

	// Create a comment on the note
	commentUID := "d3e4f5a6-b7c8-9012-cdef-3456789012ab"
	makeRequestWithSession(t, router, "POST", "/v1/sync/comments/push", pushReq{
		Items: []map[string]any{
			{
				"uid":        commentUID,
				"content":    "Comment on note",
				"parentType": "note",
				"parentUid":  noteUID,
				"updatedTs":  "2025-11-03T10:01:00Z",
				"sync":       map[string]any{"version": float64(1)},
			},
		},
	}, sessionID)

	// Delete the parent note
	makeRequestWithSession(t, router, "POST", "/v1/sync/notes/push", pushReq{
		Items: []map[string]any{
			{
				"uid":       noteUID,
				"title":     "Note to be deleted",
				"updatedTs": "2025-11-03T10:02:00Z",
				"sync": map[string]any{
					"version":   float64(1),
					"isDeleted": true,
					"deletedAt": "2025-11-03T10:02:00Z",
				},
			},
		},
	}, sessionID)

	// Now delete the comment (should succeed even though parent is deleted)
	rec := makeRequestWithSession(t, router, "POST", "/v1/sync/comments/push", pushReq{
		Items: []map[string]any{
			{
				"uid":        commentUID,
				"content":    "Comment on note",
				"parentType": "note",
				"parentUid":  noteUID,
				"updatedTs":  "2025-11-03T10:03:00Z",
				"sync": map[string]any{
					"version":   float64(1),
					"isDeleted": true,
					"deletedAt": "2025-11-03T10:03:00Z",
				},
			},
		},
	}, sessionID)

	if rec.Code != 200 {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var acks []pushAck
	if err := json.NewDecoder(rec.Body).Decode(&acks); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(acks) != 1 {
		t.Fatalf("Expected 1 ack, got %d", len(acks))
	}

	// Should succeed (no error) and version should increment to 2
	if acks[0].Error != "" {
		t.Errorf("Expected no error when deleting comment after parent deleted, got: %s", acks[0].Error)
	}

	if acks[0].Version != 2 {
		t.Errorf("Expected version 2 after deletion, got %d", acks[0].Version)
	}

	t.Logf("Comment deletion succeeded after parent deleted (version=%d)", acks[0].Version)
}

func TestPullComments_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	// Clean up tables before test
	_, _ = pool.Exec(context.Background(), "DELETE FROM comment")
	_, _ = pool.Exec(context.Background(), "DELETE FROM task")
	_, _ = pool.Exec(context.Background(), "DELETE FROM note")

	srv := &Server{DB: pool}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Create a session for this test suite
	sessionID := createTestSession(t, router)

	// Setup parent entities
	noteUID, _ := setupCommentTest(t, router, sessionID)

	// Push some comments
	makeRequestWithSession(t, router, "POST", "/v1/sync/comments/push", pushReq{
		Items: []map[string]any{
			{
				"uid":        "c1d2e3f4-a1b2-3c4d-5e6f-7a8b9c0d1e2f",
				"content":    "Comment 1",
				"parentType": "note",
				"parentUid":  noteUID,
				"updatedTs":  "2025-11-03T10:00:00Z",
				"sync":       map[string]any{"version": float64(1)},
			},
			{
				"uid":        "d2e3f4a5-b2c3-4d5e-6f7a-8b9c0d1e2f3a",
				"content":    "Comment 2",
				"parentType": "note",
				"parentUid":  noteUID,
				"updatedTs":  "2025-11-03T10:01:00Z",
				"sync":       map[string]any{"version": float64(1)},
			},
		},
	}, sessionID)

	tests := []struct {
		name       string
		query      string
		wantStatus int
		checkResp  func(*testing.T, pullResp)
	}{
		{
			name:       "pull all comments",
			query:      "?limit=100",
			wantStatus: 200,
			checkResp: func(t *testing.T, resp pullResp) {
				if len(resp.Upserts) != 2 {
					t.Errorf("Expected 2 upserts, got %d", len(resp.Upserts))
				}
				if len(resp.Deletes) != 0 {
					t.Errorf("Expected 0 deletes, got %d", len(resp.Deletes))
				}
				if resp.NextCursor == nil {
					t.Error("Expected nextCursor to be set")
				}
			},
		},
		{
			name:       "pull with limit 1",
			query:      "?limit=1",
			wantStatus: 200,
			checkResp: func(t *testing.T, resp pullResp) {
				if len(resp.Upserts) != 1 {
					t.Errorf("Expected 1 upsert, got %d", len(resp.Upserts))
				}
				if resp.NextCursor == nil {
					t.Error("Expected nextCursor to be set for pagination")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := makeRequestWithSession(t, router, "GET", "/v1/sync/comments/pull"+tt.query, nil, sessionID)

			if rec.Code != tt.wantStatus {
				t.Errorf("Status = %d, want %d", rec.Code, tt.wantStatus)
			}

			var resp pullResp
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if tt.checkResp != nil {
				tt.checkResp(t, resp)
			}
		})
	}
}

func TestPushPullRoundTrip_Comments_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	// Clean up tables before test
	_, _ = pool.Exec(context.Background(), "DELETE FROM comment")
	_, _ = pool.Exec(context.Background(), "DELETE FROM task")
	_, _ = pool.Exec(context.Background(), "DELETE FROM note")

	srv := &Server{DB: pool}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Create a session for this test suite
	sessionID := createTestSession(t, router)

	// Setup parent entities
	noteUID, _ := setupCommentTest(t, router, sessionID)

	// Push a comment
	original := map[string]any{
		"uid":        "c1d2e3f4-a1b2-3c4d-5e6f-7a8b9c0d1e2f",
		"content":    "Round Trip Test Comment",
		"author":     "test-user",
		"tags":       []any{"test", "integration"},
		"metadata":   map[string]any{"edited": false},
		"parentType": "note",
		"parentUid":  noteUID,
		"updatedTs":  "2025-11-03T10:00:00Z",
		"sync": map[string]any{
			"version":   float64(1),
			"isDeleted": false,
		},
	}

	makeRequestWithSession(t, router, "POST", "/v1/sync/comments/push", pushReq{Items: []map[string]any{original}}, sessionID)

	// Pull it back
	pullRec := makeRequestWithSession(t, router, "GET", "/v1/sync/comments/pull?limit=100", nil, sessionID)

	var pullResp pullResp
	if err := json.NewDecoder(pullRec.Body).Decode(&pullResp); err != nil {
		t.Fatalf("Failed to decode pull response: %v", err)
	}

	if len(pullResp.Upserts) != 1 {
		t.Fatalf("Expected 1 comment, got %d", len(pullResp.Upserts))
	}

	retrieved := pullResp.Upserts[0]

	// Verify all fields preserved
	if retrieved["content"] != original["content"] {
		t.Errorf("Content mismatch: got %v, want %v", retrieved["content"], original["content"])
	}
	if retrieved["author"] != original["author"] {
		t.Errorf("Author mismatch: got %v, want %v", retrieved["author"], original["author"])
	}
	if retrieved["parentType"] != original["parentType"] {
		t.Errorf("ParentType mismatch: got %v, want %v", retrieved["parentType"], original["parentType"])
	}
	if retrieved["parentUid"] != original["parentUid"] {
		t.Errorf("ParentUid mismatch: got %v, want %v", retrieved["parentUid"], original["parentUid"])
	}

	// Verify arrays preserved
	tags, ok := retrieved["tags"].([]any)
	if !ok || len(tags) != 2 {
		t.Errorf("Tags not preserved correctly: %v", retrieved["tags"])
	}

	// Verify nested objects preserved
	metadata, ok := retrieved["metadata"].(map[string]any)
	if !ok || metadata["edited"] != false {
		t.Errorf("Metadata not preserved correctly: %v", retrieved["metadata"])
	}
}

func TestSoftDelete_Comments_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	// Clean up tables before test
	_, _ = pool.Exec(context.Background(), "DELETE FROM comment")
	_, _ = pool.Exec(context.Background(), "DELETE FROM task")
	_, _ = pool.Exec(context.Background(), "DELETE FROM note")

	srv := &Server{DB: pool}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Create a session for this test suite
	sessionID := createTestSession(t, router)

	// Setup parent entities
	noteUID, _ := setupCommentTest(t, router, sessionID)

	// Push a comment
	makeRequestWithSession(t, router, "POST", "/v1/sync/comments/push", pushReq{
		Items: []map[string]any{
			{
				"uid":        "c1d2e3f4-a1b2-3c4d-5e6f-7a8b9c0d1e2f",
				"content":    "To be deleted",
				"parentType": "note",
				"parentUid":  noteUID,
				"updatedTs":  "2025-11-03T10:00:00Z",
				"sync":       map[string]any{"version": float64(1), "isDeleted": false},
			},
		},
	}, sessionID)

	// Delete the comment
	makeRequestWithSession(t, router, "POST", "/v1/sync/comments/push", pushReq{
		Items: []map[string]any{
			{
				"uid":        "c1d2e3f4-a1b2-3c4d-5e6f-7a8b9c0d1e2f",
				"content":    "To be deleted",
				"parentType": "note",
				"parentUid":  noteUID,
				"updatedTs":  "2025-11-03T10:01:00Z",
				"sync": map[string]any{
					"version":   float64(1),
					"isDeleted": true,
					"deletedAt": "2025-11-03T10:01:00Z",
				},
			},
		},
	}, sessionID)

	// Pull and verify it's in deletes array
	pullRec := makeRequestWithSession(t, router, "GET", "/v1/sync/comments/pull?limit=100", nil, sessionID)

	var pullResp pullResp
	json.NewDecoder(pullRec.Body).Decode(&pullResp)

	if len(pullResp.Upserts) != 0 {
		t.Errorf("Expected 0 upserts, got %d", len(pullResp.Upserts))
	}
	if len(pullResp.Deletes) != 1 {
		t.Fatalf("Expected 1 delete, got %d", len(pullResp.Deletes))
	}
	if pullResp.Deletes[0]["uid"] != "c1d2e3f4-a1b2-3c4d-5e6f-7a8b9c0d1e2f" {
		t.Errorf("Wrong comment in deletes: %v", pullResp.Deletes[0])
	}
}
