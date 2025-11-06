package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/erauner12/toolbridge-api/internal/auth"
)

// setupChatMessageTest creates a chat for testing chat messages
func setupChatMessageTest(t *testing.T, router http.Handler, sessionID string) string {
	t.Helper()

	// Create a chat
	chatUID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	makeRequestWithSession(t, router, "POST", "/v1/sync/chats/push", pushReq{
		Items: []map[string]any{
			{
				"uid":       chatUID,
				"title":     "Test Chat for Messages",
				"updatedTs": "2025-11-03T10:00:00Z",
				"sync":      map[string]any{"version": float64(1)},
			},
		},
	}, sessionID)

	return chatUID
}

func TestPushChatMessages_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	// Clean up tables before test
	_, _ = pool.Exec(context.Background(), "DELETE FROM chat_message")
	_, _ = pool.Exec(context.Background(), "DELETE FROM chat")

	srv := &Server{DB: pool}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Create a session for this test suite
	sessionID := createTestSession(t, router)

	// Setup parent chat
	chatUID := setupChatMessageTest(t, router, sessionID)

	tests := []struct {
		name       string
		body       pushReq
		wantStatus int
		checkResp  func(*testing.T, []pushAck)
	}{
		{
			name: "push message for chat",
			body: pushReq{
				Items: []map[string]any{
					{
						"uid":       "c1d2e3f4-a1b2-3c4d-5e6f-7a8b9c0d1e2f",
						"content":   "Test message on chat",
						"chatUid":   chatUID,
						"updatedTs": "2025-11-03T10:00:00Z",
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
						"uid":       "c1d2e3f4-a1b2-3c4d-5e6f-7a8b9c0d1e2f",
						"content":   "Test message on chat",
						"chatUid":   chatUID,
						"updatedTs": "2025-11-03T10:00:00Z",
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
						"uid":       "c1d2e3f4-a1b2-3c4d-5e6f-7a8b9c0d1e2f",
						"content":   "Updated message",
						"chatUid":   chatUID,
						"updatedTs": "2025-11-03T10:01:00Z", // Newer timestamp
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
			name: "push with missing chatUid",
			body: pushReq{
				Items: []map[string]any{
					{
						"uid":       "e3f4a5b6-c3d4-5e6f-7a8b-9c0d1e2f3a4b",
						"content":   "Message without parent",
						"updatedTs": "2025-11-03T10:00:00Z",
					},
				},
			},
			wantStatus: 200,
			checkResp: func(t *testing.T, acks []pushAck) {
				if acks[0].Error == "" {
					t.Error("Expected error for missing chatUid")
				}
			},
		},
		{
			name: "push with non-existent parent chat",
			body: pushReq{
				Items: []map[string]any{
					{
						"uid":       "f4a5b6c7-d4e5-6f7a-8b9c-0d1e2f3a4b5c",
						"content":   "Message on non-existent chat",
						"chatUid":   "99999999-9999-9999-9999-999999999999",
						"updatedTs": "2025-11-03T10:00:00Z",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := makeRequestWithSession(t, router, "POST", "/v1/sync/chat_messages/push", tt.body, sessionID)

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

func TestPushChatMessagesOnDeletedParent_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	// Clean up tables before test
	_, _ = pool.Exec(context.Background(), "DELETE FROM chat_message")
	_, _ = pool.Exec(context.Background(), "DELETE FROM chat")

	srv := &Server{DB: pool}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Create a session for this test suite
	sessionID := createTestSession(t, router)

	// Create a chat
	chatUID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	makeRequestWithSession(t, router, "POST", "/v1/sync/chats/push", pushReq{
		Items: []map[string]any{
			{
				"uid":       chatUID,
				"title":     "Chat to be deleted",
				"updatedTs": "2025-11-03T10:00:00Z",
				"sync":      map[string]any{"version": float64(1)},
			},
		},
	}, sessionID)

	// Soft delete the chat
	makeRequestWithSession(t, router, "POST", "/v1/sync/chats/push", pushReq{
		Items: []map[string]any{
			{
				"uid":       chatUID,
				"title":     "Chat to be deleted",
				"updatedTs": "2025-11-03T10:01:00Z",
				"sync": map[string]any{
					"version":   float64(1),
					"isDeleted": true,
					"deletedAt": "2025-11-03T10:01:00Z",
				},
			},
		},
	}, sessionID)

	// Try to create a message on the soft-deleted chat (should fail)
	messageRec := makeRequestWithSession(t, router, "POST", "/v1/sync/chat_messages/push", pushReq{
		Items: []map[string]any{
			{
				"uid":       "c1d2e3f4-a1b2-3c4d-5e6f-7a8b9c0d1e2f",
				"content":   "Message on deleted chat",
				"chatUid":   chatUID,
				"updatedTs": "2025-11-03T10:02:00Z",
				"sync": map[string]any{
					"version": float64(1),
				},
			},
		},
	}, sessionID)

	var acks []pushAck
	if err := json.NewDecoder(messageRec.Body).Decode(&acks); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Should get an error about parent not found
	if len(acks) != 1 {
		t.Fatalf("Expected 1 ack, got %d", len(acks))
	}
	if acks[0].Error == "" {
		t.Error("Expected error for message on deleted parent, got none")
	}
	if acks[0].Error != "" {
		t.Logf("Got expected error: %s", acks[0].Error)
	}
}

func TestDeleteChatMessageAfterParentDeleted_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	// Clean up
	_, _ = pool.Exec(context.Background(), "DELETE FROM chat_message")
	_, _ = pool.Exec(context.Background(), "DELETE FROM chat")

	srv := &Server{DB: pool}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Create a session for this test suite
	sessionID := createTestSession(t, router)

	// Create a chat
	chatUID := "b2c3d4e5-f6a7-8901-bcde-f2345678901a"
	makeRequestWithSession(t, router, "POST", "/v1/sync/chats/push", pushReq{
		Items: []map[string]any{
			{
				"uid":       chatUID,
				"title":     "Chat to be deleted",
				"content":   "This chat will be deleted",
				"updatedTs": "2025-11-03T10:00:00Z",
				"sync":      map[string]any{"version": float64(1)},
			},
		},
	}, sessionID)

	// Create a message on the chat
	messageUID := "d3e4f5a6-b7c8-9012-cdef-3456789012ab"
	makeRequestWithSession(t, router, "POST", "/v1/sync/chat_messages/push", pushReq{
		Items: []map[string]any{
			{
				"uid":       messageUID,
				"content":   "Message on chat",
				"chatUid":   chatUID,
				"updatedTs": "2025-11-03T10:01:00Z",
				"sync":      map[string]any{"version": float64(1)},
			},
		},
	}, sessionID)

	// Delete the parent chat
	makeRequestWithSession(t, router, "POST", "/v1/sync/chats/push", pushReq{
		Items: []map[string]any{
			{
				"uid":       chatUID,
				"title":     "Chat to be deleted",
				"updatedTs": "2025-11-03T10:02:00Z",
				"sync": map[string]any{
					"version":   float64(1),
					"isDeleted": true,
					"deletedAt": "2025-11-03T10:02:00Z",
				},
			},
		},
	}, sessionID)

	// Now delete the message (should succeed even though parent is deleted)
	rec := makeRequestWithSession(t, router, "POST", "/v1/sync/chat_messages/push", pushReq{
		Items: []map[string]any{
			{
				"uid":       messageUID,
				"content":   "Message on chat",
				"chatUid":   chatUID,
				"updatedTs": "2025-11-03T10:03:00Z",
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
		t.Errorf("Expected no error when deleting message after parent deleted, got: %s", acks[0].Error)
	}

	if acks[0].Version != 2 {
		t.Errorf("Expected version 2 after deletion, got %d", acks[0].Version)
	}

	t.Logf("Message deletion succeeded after parent deleted (version=%d)", acks[0].Version)
}

func TestPullChatMessages_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	// Clean up tables before test
	_, _ = pool.Exec(context.Background(), "DELETE FROM chat_message")
	_, _ = pool.Exec(context.Background(), "DELETE FROM chat")

	srv := &Server{DB: pool}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Create a session for this test suite
	sessionID := createTestSession(t, router)

	// Setup parent chat
	chatUID := setupChatMessageTest(t, router, sessionID)

	// Push some messages
	makeRequestWithSession(t, router, "POST", "/v1/sync/chat_messages/push", pushReq{
		Items: []map[string]any{
			{
				"uid":       "c1d2e3f4-a1b2-3c4d-5e6f-7a8b9c0d1e2f",
				"content":   "Message 1",
				"chatUid":   chatUID,
				"updatedTs": "2025-11-03T10:00:00Z",
				"sync":      map[string]any{"version": float64(1)},
			},
			{
				"uid":       "d2e3f4a5-b2c3-4d5e-6f7a-8b9c0d1e2f3a",
				"content":   "Message 2",
				"chatUid":   chatUID,
				"updatedTs": "2025-11-03T10:01:00Z",
				"sync":      map[string]any{"version": float64(1)},
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
			name:       "pull all messages",
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
			rec := makeRequestWithSession(t, router, "GET", "/v1/sync/chat_messages/pull"+tt.query, nil, sessionID)

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

func TestPushPullRoundTrip_ChatMessages_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	// Clean up tables before test
	_, _ = pool.Exec(context.Background(), "DELETE FROM chat_message")
	_, _ = pool.Exec(context.Background(), "DELETE FROM chat")

	srv := &Server{DB: pool}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Create a session for this test suite
	sessionID := createTestSession(t, router)

	// Setup parent chat
	chatUID := setupChatMessageTest(t, router, sessionID)

	// Push a message
	original := map[string]any{
		"uid":       "c1d2e3f4-a1b2-3c4d-5e6f-7a8b9c0d1e2f",
		"content":   "Round Trip Test Message",
		"author":    "test-user",
		"tags":      []any{"test", "integration"},
		"metadata":  map[string]any{"edited": false},
		"chatUid":   chatUID,
		"updatedTs": "2025-11-03T10:00:00Z",
		"sync": map[string]any{
			"version":   float64(1),
			"isDeleted": false,
		},
	}

	makeRequestWithSession(t, router, "POST", "/v1/sync/chat_messages/push", pushReq{Items: []map[string]any{original}}, sessionID)

	// Pull it back
	pullRec := makeRequestWithSession(t, router, "GET", "/v1/sync/chat_messages/pull?limit=100", nil, sessionID)

	var pullResp pullResp
	if err := json.NewDecoder(pullRec.Body).Decode(&pullResp); err != nil {
		t.Fatalf("Failed to decode pull response: %v", err)
	}

	if len(pullResp.Upserts) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(pullResp.Upserts))
	}

	retrieved := pullResp.Upserts[0]

	// Verify all fields preserved
	if retrieved["content"] != original["content"] {
		t.Errorf("Content mismatch: got %v, want %v", retrieved["content"], original["content"])
	}
	if retrieved["author"] != original["author"] {
		t.Errorf("Author mismatch: got %v, want %v", retrieved["author"], original["author"])
	}
	if retrieved["chatUid"] != original["chatUid"] {
		t.Errorf("ChatUid mismatch: got %v, want %v", retrieved["chatUid"], original["chatUid"])
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

func TestSoftDelete_ChatMessages_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	// Clean up tables before test
	_, _ = pool.Exec(context.Background(), "DELETE FROM chat_message")
	_, _ = pool.Exec(context.Background(), "DELETE FROM chat")

	srv := &Server{DB: pool}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Create a session for this test suite
	sessionID := createTestSession(t, router)

	// Setup parent chat
	chatUID := setupChatMessageTest(t, router, sessionID)

	// Push a message
	makeRequestWithSession(t, router, "POST", "/v1/sync/chat_messages/push", pushReq{
		Items: []map[string]any{
			{
				"uid":       "c1d2e3f4-a1b2-3c4d-5e6f-7a8b9c0d1e2f",
				"content":   "To be deleted",
				"chatUid":   chatUID,
				"updatedTs": "2025-11-03T10:00:00Z",
				"sync":      map[string]any{"version": float64(1), "isDeleted": false},
			},
		},
	}, sessionID)

	// Delete the message
	makeRequestWithSession(t, router, "POST", "/v1/sync/chat_messages/push", pushReq{
		Items: []map[string]any{
			{
				"uid":       "c1d2e3f4-a1b2-3c4d-5e6f-7a8b9c0d1e2f",
				"content":   "To be deleted",
				"chatUid":   chatUID,
				"updatedTs": "2025-11-03T10:01:00Z",
				"sync": map[string]any{
					"version":   float64(1),
					"isDeleted": true,
					"deletedAt": "2025-11-03T10:01:00Z",
				},
			},
		},
	}, sessionID)

	// Pull and verify it's in deletes array
	pullRec := makeRequestWithSession(t, router, "GET", "/v1/sync/chat_messages/pull?limit=100", nil, sessionID)

	var pullResp pullResp
	json.NewDecoder(pullRec.Body).Decode(&pullResp)

	if len(pullResp.Upserts) != 0 {
		t.Errorf("Expected 0 upserts, got %d", len(pullResp.Upserts))
	}
	if len(pullResp.Deletes) != 1 {
		t.Fatalf("Expected 1 delete, got %d", len(pullResp.Deletes))
	}
	if pullResp.Deletes[0]["uid"] != "c1d2e3f4-a1b2-3c4d-5e6f-7a8b9c0d1e2f" {
		t.Errorf("Wrong message in deletes: %v", pullResp.Deletes[0])
	}
}
