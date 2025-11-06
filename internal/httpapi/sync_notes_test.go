package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/erauner12/toolbridge-api/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Test database URL from environment or skip if not set
func getTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration tests")
	}

	pool, err := db.Open(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	// Clean up notes table before each test
	_, err = pool.Exec(context.Background(), "DELETE FROM note")
	if err != nil {
		t.Fatalf("Failed to clean notes table: %v", err)
	}

	return pool
}

func TestPushNotes_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	srv := &Server{DB: pool, RateLimitConfig: DefaultRateLimitConfig}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Create a session for this test suite
	sessionID := createTestSession(t, router)

	tests := []struct {
		name       string
		body       pushReq
		wantStatus int
		checkResp  func(*testing.T, []pushAck)
	}{
		{
			name: "push single note",
			body: pushReq{
				Items: []map[string]any{
					{
						"uid":       "c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f",
						"title":     "Test Note",
						"content":   "Test content",
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
						"uid":       "c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f",
						"title":     "Test Note",
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
						"uid":       "c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f",
						"title":     "Updated Note",
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
			name: "push invalid item (missing uid)",
			body: pushReq{
				Items: []map[string]any{
					{
						"title":     "No UID",
						"updatedTs": "2025-11-03T10:00:00Z",
					},
				},
			},
			wantStatus: 200,
			checkResp: func(t *testing.T, acks []pushAck) {
				if acks[0].Error == "" {
					t.Error("Expected error for missing UID")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := makeRequestWithSession(t, router, "POST", "/v1/sync/notes/push", tt.body, sessionID)

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

func TestPullNotes_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	srv := &Server{DB: pool, RateLimitConfig: DefaultRateLimitConfig}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Create a session for this test suite
	sessionID := createTestSession(t, router)

	// First, push some notes
	makeRequestWithSession(t, router, "POST", "/v1/sync/notes/push", pushReq{
		Items: []map[string]any{
			{
				"uid":       "c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f",
				"title":     "Note 1",
				"updatedTs": "2025-11-03T10:00:00Z",
				"sync":      map[string]any{"version": float64(1)},
			},
			{
				"uid":       "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
				"title":     "Note 2",
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
			name:       "pull all notes",
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
		{
			name:       "pull with cursor (page 2)",
			query:      "?limit=1&cursor=MTczMDYzMTYwMDAwMHxjMWQ5YjdkYy1hMWIyLTRjM2QtOWU4Zi03YTZiNWM0ZDNlMmY",
			wantStatus: 200,
			checkResp: func(t *testing.T, resp pullResp) {
				// Should get the second note (or none if cursor is past it)
				if len(resp.Upserts) > 1 {
					t.Errorf("Expected at most 1 upsert, got %d", len(resp.Upserts))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := makeRequestWithSession(t, router, "GET", "/v1/sync/notes/pull"+tt.query, nil, sessionID)

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

func TestPushPullRoundTrip_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	srv := &Server{DB: pool, RateLimitConfig: DefaultRateLimitConfig}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Create a session for this test suite
	sessionID := createTestSession(t, router)

	// Push a note
	original := map[string]any{
		"uid":       "c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f",
		"title":     "Round Trip Test",
		"content":   "This should survive the round trip",
		"tags":      []any{"test", "integration"},
		"metadata":  map[string]any{"author": "test-user"},
		"updatedTs": "2025-11-03T10:00:00Z",
		"sync": map[string]any{
			"version":   float64(1),
			"isDeleted": false,
		},
	}

	makeRequestWithSession(t, router, "POST", "/v1/sync/notes/push", pushReq{Items: []map[string]any{original}}, sessionID)

	// Pull it back
	pullRec := makeRequestWithSession(t, router, "GET", "/v1/sync/notes/pull?limit=100", nil, sessionID)

	var pullResp pullResp
	if err := json.NewDecoder(pullRec.Body).Decode(&pullResp); err != nil {
		t.Fatalf("Failed to decode pull response: %v", err)
	}

	if len(pullResp.Upserts) != 1 {
		t.Fatalf("Expected 1 note, got %d", len(pullResp.Upserts))
	}

	retrieved := pullResp.Upserts[0]

	// Verify all fields preserved
	if retrieved["title"] != original["title"] {
		t.Errorf("Title mismatch: got %v, want %v", retrieved["title"], original["title"])
	}
	if retrieved["content"] != original["content"] {
		t.Errorf("Content mismatch: got %v, want %v", retrieved["content"], original["content"])
	}

	// Verify arrays preserved
	tags, ok := retrieved["tags"].([]any)
	if !ok || len(tags) != 2 {
		t.Errorf("Tags not preserved correctly: %v", retrieved["tags"])
	}

	// Verify nested objects preserved
	metadata, ok := retrieved["metadata"].(map[string]any)
	if !ok || metadata["author"] != "test-user" {
		t.Errorf("Metadata not preserved correctly: %v", retrieved["metadata"])
	}
}

func TestSoftDelete_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	srv := &Server{DB: pool, RateLimitConfig: DefaultRateLimitConfig}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Create a session for this test suite
	sessionID := createTestSession(t, router)

	// Push a note
	makeRequestWithSession(t, router, "POST", "/v1/sync/notes/push", pushReq{
		Items: []map[string]any{
			{
				"uid":       "c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f",
				"title":     "To be deleted",
				"updatedTs": "2025-11-03T10:00:00Z",
				"sync":      map[string]any{"version": float64(1), "isDeleted": false},
			},
		},
	}, sessionID)

	// Delete the note
	makeRequestWithSession(t, router, "POST", "/v1/sync/notes/push", pushReq{
		Items: []map[string]any{
			{
				"uid":       "c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f",
				"title":     "To be deleted",
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
	pullRec := makeRequestWithSession(t, router, "GET", "/v1/sync/notes/pull?limit=100", nil, sessionID)

	var pullResp pullResp
	json.NewDecoder(pullRec.Body).Decode(&pullResp)

	if len(pullResp.Upserts) != 0 {
		t.Errorf("Expected 0 upserts, got %d", len(pullResp.Upserts))
	}
	if len(pullResp.Deletes) != 1 {
		t.Fatalf("Expected 1 delete, got %d", len(pullResp.Deletes))
	}
	if pullResp.Deletes[0]["uid"] != "c1d9b7dc-a1b2-4c3d-9e8f-7a6b5c4d3e2f" {
		t.Errorf("Wrong note in deletes: %v", pullResp.Deletes[0])
	}
}
