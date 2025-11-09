//go:build grpc
// +build grpc

package grpcapi

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/erauner12/toolbridge-api/internal/db"
	"github.com/erauner12/toolbridge-api/internal/service/syncservice"
	syncv1 "github.com/erauner12/toolbridge-api/gen/go/sync/v1"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/structpb"
)

const bufSize = 1024 * 1024

var lis *bufconn.Listener

// getTestDB returns a connection to the test database
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

	// Clean up all tables before each test
	_, err = pool.Exec(context.Background(), `
		DELETE FROM chat_message;
		DELETE FROM chat;
		DELETE FROM comment;
		DELETE FROM task;
		DELETE FROM note;
		DELETE FROM owner_state;
		DELETE FROM app_user;
	`)
	if err != nil {
		t.Fatalf("Failed to clean test database: %v", err)
	}

	return pool
}

// setupTestGrpcServer creates an in-process gRPC server for testing
func setupTestGrpcServer(t *testing.T, pool *pgxpool.Pool) *grpc.Server {
	t.Helper()

	lis = bufconn.Listen(bufSize)

	// Create gRPC server with full interceptor chain
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			RecoveryInterceptor(),
			CorrelationIDInterceptor(),
			AuthInterceptor(pool, auth.JWTCfg{HS256Secret: "test-secret", DevMode: true}),
			SessionInterceptor(),
			EpochInterceptor(pool),
			LoggingInterceptor(),
		),
	)

	// Create and register server implementation
	srv := &Server{
		DB:             pool,
		NoteSvc:        syncservice.NewNoteService(pool),
		TaskSvc:        syncservice.NewTaskService(pool),
		CommentSvc:     syncservice.NewCommentService(pool),
		ChatSvc:        syncservice.NewChatService(pool),
		ChatMessageSvc: syncservice.NewChatMessageService(pool),
	}

	syncv1.RegisterSyncServiceServer(grpcServer, srv)
	syncv1.RegisterNoteSyncServiceServer(grpcServer, srv)
	syncv1.RegisterTaskSyncServiceServer(grpcServer, &TaskServer{Server: srv})
	syncv1.RegisterCommentSyncServiceServer(grpcServer, &CommentServer{Server: srv})
	syncv1.RegisterChatSyncServiceServer(grpcServer, &ChatServer{Server: srv})
	syncv1.RegisterChatMessageSyncServiceServer(grpcServer, &ChatMessageServer{Server: srv})

	// Start server in background
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			t.Logf("gRPC server error: %v", err)
		}
	}()

	return grpcServer
}

// bufDialer returns a dialer that connects to the in-process server
func bufDialer(context.Context, string) (net.Conn, error) {
	return lis.Dial()
}

// createTestClients creates all gRPC clients for testing
func createTestClients(t *testing.T) (
	*grpc.ClientConn,
	syncv1.SyncServiceClient,
	syncv1.NoteSyncServiceClient,
	syncv1.TaskSyncServiceClient,
	syncv1.CommentSyncServiceClient,
	syncv1.ChatSyncServiceClient,
	syncv1.ChatMessageSyncServiceClient,
) {
	t.Helper()

	conn, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}

	return conn,
		syncv1.NewSyncServiceClient(conn),
		syncv1.NewNoteSyncServiceClient(conn),
		syncv1.NewTaskSyncServiceClient(conn),
		syncv1.NewCommentSyncServiceClient(conn),
		syncv1.NewChatSyncServiceClient(conn),
		syncv1.NewChatMessageSyncServiceClient(conn)
}

// createDevModeContext creates a context with dev mode authentication
func createDevModeContext(userID string) context.Context {
	md := metadata.New(map[string]string{
		"x-debug-sub":     userID,
		"x-correlation-id": "test-correlation-id",
	})
	return metadata.NewOutgoingContext(context.Background(), md)
}

// createAuthenticatedContext creates a context with session and epoch headers
func createAuthenticatedContext(userID, sessionID string, epoch int) context.Context {
	md := metadata.New(map[string]string{
		"x-debug-sub":     userID,
		"x-sync-session":  sessionID,
		"x-sync-epoch":    fmt.Sprintf("%d", epoch), // Convert int to string
		"x-correlation-id": "test-correlation-id",
	})
	return metadata.NewOutgoingContext(context.Background(), md)
}

// ===== Auth Interceptor Tests =====

func TestAuthInterceptor_DevMode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	grpcServer := setupTestGrpcServer(t, pool)
	defer grpcServer.Stop()

	conn, syncClient, _, _, _, _, _ := createTestClients(t)
	defer conn.Close()

	ctx := createDevModeContext("test-user-123")

	// Call GetServerInfo (auth required, session exempt)
	resp, err := syncClient.GetServerInfo(ctx, &syncv1.GetServerInfoRequest{})
	if err != nil {
		t.Fatalf("GetServerInfo failed: %v", err)
	}

	if resp.ApiVersion != "1.1" {
		t.Errorf("Expected API version 1.1, got %s", resp.ApiVersion)
	}
}

func TestAuthInterceptor_MissingAuth(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	grpcServer := setupTestGrpcServer(t, pool)
	defer grpcServer.Stop()

	conn, syncClient, _, _, _, _, _ := createTestClients(t)
	defer conn.Close()

	// Call without authentication
	_, err := syncClient.GetServerInfo(context.Background(), &syncv1.GetServerInfoRequest{})
	if err == nil {
		t.Fatal("Expected auth error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("Expected gRPC status error, got %T", err)
	}

	if st.Code() != codes.Unauthenticated {
		t.Errorf("Expected Unauthenticated code, got %v", st.Code())
	}
}

// ===== Session Interceptor Tests =====

func TestSessionInterceptor_BeginSession(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	grpcServer := setupTestGrpcServer(t, pool)
	defer grpcServer.Stop()

	conn, syncClient, _, _, _, _, _ := createTestClients(t)
	defer conn.Close()

	ctx := createDevModeContext("test-user-session")

	// Begin session
	resp, err := syncClient.BeginSession(ctx, &syncv1.BeginSessionRequest{})
	if err != nil {
		t.Fatalf("BeginSession failed: %v", err)
	}

	if resp.Id == "" {
		t.Error("Expected session ID, got empty string")
	}

	if resp.Epoch <= 0 {
		t.Errorf("Expected positive epoch, got %d", resp.Epoch)
	}
}

func TestSessionInterceptor_MissingSession(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	grpcServer := setupTestGrpcServer(t, pool)
	defer grpcServer.Stop()

	conn, _, noteClient, _, _, _, _ := createTestClients(t)
	defer conn.Close()

	ctx := createDevModeContext("test-user-no-session")

	// Try to push without session
	_, err := noteClient.Push(ctx, &syncv1.PushRequest{Items: []*structpb.Struct{}})
	if err == nil {
		t.Fatal("Expected session required error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("Expected gRPC status error, got %T", err)
	}

	if st.Code() != codes.FailedPrecondition {
		t.Errorf("Expected FailedPrecondition code, got %v", st.Code())
	}

	if !contains(st.Message(), "X-Sync-Session") {
		t.Errorf("Expected session error message, got: %s", st.Message())
	}
}

// ===== Epoch Interceptor Tests =====

func TestEpochInterceptor_EpochMismatch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	grpcServer := setupTestGrpcServer(t, pool)
	defer grpcServer.Stop()

	conn, syncClient, noteClient, _, _, _, _ := createTestClients(t)
	defer conn.Close()

	userID := "test-user-epoch-mismatch"
	ctx := createDevModeContext(userID)

	// Begin session (creates epoch 1)
	session, err := syncClient.BeginSession(ctx, &syncv1.BeginSessionRequest{})
	if err != nil {
		t.Fatalf("BeginSession failed: %v", err)
	}

	// Try to push with wrong epoch
	wrongEpochCtx := createAuthenticatedContext(userID, session.Id, 999)
	_, err = noteClient.Push(wrongEpochCtx, &syncv1.PushRequest{Items: []*structpb.Struct{}})
	if err == nil {
		t.Fatal("Expected epoch mismatch error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("Expected gRPC status error, got %T", err)
	}

	if st.Code() != codes.FailedPrecondition {
		t.Errorf("Expected FailedPrecondition code, got %v", st.Code())
	}

	if !contains(st.Message(), "Epoch mismatch") {
		t.Errorf("Expected epoch mismatch message, got: %s", st.Message())
	}
}

// ===== Core RPC Tests =====

func TestGetServerInfo(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	grpcServer := setupTestGrpcServer(t, pool)
	defer grpcServer.Stop()

	conn, syncClient, _, _, _, _, _ := createTestClients(t)
	defer conn.Close()

	ctx := createDevModeContext("test-user-info")

	resp, err := syncClient.GetServerInfo(ctx, &syncv1.GetServerInfoRequest{})
	if err != nil {
		t.Fatalf("GetServerInfo failed: %v", err)
	}

	// Verify response
	if resp.ApiVersion != "1.1" {
		t.Errorf("Expected API version 1.1, got %s", resp.ApiVersion)
	}

	if resp.ServerTime == nil {
		t.Error("Expected server time to be set")
	}

	// Check entities
	expectedEntities := []string{"notes", "tasks", "comments", "chats", "chat_messages"}
	for _, entity := range expectedEntities {
		if _, ok := resp.Entities[entity]; !ok {
			t.Errorf("Expected entity %s to be present", entity)
		}
	}

	// Check rate limit
	if resp.RateLimit == nil {
		t.Error("Expected rate limit to be set")
	} else {
		if resp.RateLimit.WindowSeconds <= 0 {
			t.Errorf("Expected positive window seconds, got %d", resp.RateLimit.WindowSeconds)
		}
	}

	// Check hints
	if resp.Hints == nil {
		t.Error("Expected hints to be set")
	} else {
		if resp.Hints.RecommendedBatch <= 0 {
			t.Errorf("Expected positive recommended batch, got %d", resp.Hints.RecommendedBatch)
		}
	}
}

func TestBeginAndEndSession(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	grpcServer := setupTestGrpcServer(t, pool)
	defer grpcServer.Stop()

	conn, syncClient, _, _, _, _, _ := createTestClients(t)
	defer conn.Close()

	ctx := createDevModeContext("test-user-session-lifecycle")

	// Begin session
	beginResp, err := syncClient.BeginSession(ctx, &syncv1.BeginSessionRequest{})
	if err != nil {
		t.Fatalf("BeginSession failed: %v", err)
	}

	if beginResp.Id == "" {
		t.Error("Expected session ID")
	}

	if beginResp.Epoch <= 0 {
		t.Errorf("Expected positive epoch, got %d", beginResp.Epoch)
	}

	// End session
	endCtx := createAuthenticatedContext("test-user-session-lifecycle", beginResp.Id, int(beginResp.Epoch))
	_, err = syncClient.EndSession(endCtx, &syncv1.EndSessionRequest{
		SessionId: beginResp.Id, // Server requires sessionId in request body
	})
	if err != nil {
		t.Fatalf("EndSession failed: %v", err)
	}

	// EndSessionResponse is empty, success is indicated by no error
}

func TestWipeAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	grpcServer := setupTestGrpcServer(t, pool)
	defer grpcServer.Stop()

	conn, syncClient, noteClient, _, _, _, _ := createTestClients(t)
	defer conn.Close()

	userID := "test-user-wipe"
	ctx := createDevModeContext(userID)

	// Begin session
	session, err := syncClient.BeginSession(ctx, &syncv1.BeginSessionRequest{})
	if err != nil {
		t.Fatalf("BeginSession failed: %v", err)
	}

	// Push a note
	authCtx := createAuthenticatedContext(userID, session.Id, int(session.Epoch))
	noteItem, _ := structpb.NewStruct(map[string]interface{}{
		"uid":       "dddd0000-0000-0000-0000-000000000000",
		"title":     "Test Note",
		"updatedTs": "2025-11-09T10:00:00Z",
		"sync": map[string]interface{}{
			"version": 1,
		},
	})

	_, err = noteClient.Push(authCtx, &syncv1.PushRequest{Items: []*structpb.Struct{noteItem}})
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Wipe account
	wipeResp, err := syncClient.WipeAccount(authCtx, &syncv1.WipeAccountRequest{
		Confirm: "WIPE", // Server requires explicit confirmation
	})
	if err != nil {
		t.Fatalf("WipeAccount failed: %v", err)
	}

	if wipeResp.Epoch <= session.Epoch {
		t.Errorf("Expected epoch to increment, got %d (was %d)", wipeResp.Epoch, session.Epoch)
	}

	// Verify notes are deleted by pulling
	newSession, err := syncClient.BeginSession(ctx, &syncv1.BeginSessionRequest{})
	if err != nil {
		t.Fatalf("BeginSession after wipe failed: %v", err)
	}

	newAuthCtx := createAuthenticatedContext(userID, newSession.Id, int(newSession.Epoch))
	pullResp, err := noteClient.Pull(newAuthCtx, &syncv1.PullRequest{Limit: 100})
	if err != nil {
		t.Fatalf("Pull after wipe failed: %v", err)
	}

	if len(pullResp.Upserts) != 0 {
		t.Errorf("Expected 0 notes after wipe, got %d", len(pullResp.Upserts))
	}
}

func TestGetSyncState(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	grpcServer := setupTestGrpcServer(t, pool)
	defer grpcServer.Stop()

	conn, syncClient, _, _, _, _, _ := createTestClients(t)
	defer conn.Close()

	userID := "test-user-sync-state"
	ctx := createDevModeContext(userID)

	// Begin session (creates owner_state)
	session, err := syncClient.BeginSession(ctx, &syncv1.BeginSessionRequest{})
	if err != nil {
		t.Fatalf("BeginSession failed: %v", err)
	}

	// Get sync state
	stateCtx := createAuthenticatedContext(userID, session.Id, int(session.Epoch))
	stateResp, err := syncClient.GetSyncState(stateCtx, &syncv1.GetSyncStateRequest{})
	if err != nil {
		t.Fatalf("GetSyncState failed: %v", err)
	}

	if stateResp.Epoch != session.Epoch {
		t.Errorf("Expected epoch %d, got %d", session.Epoch, stateResp.Epoch)
	}

	// last_wipe_at may be nil if user never wiped
	// Just verify we got a response
}

// ===== Entity RPC Tests - Notes =====

func TestNotePush(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	grpcServer := setupTestGrpcServer(t, pool)
	defer grpcServer.Stop()

	conn, syncClient, noteClient, _, _, _, _ := createTestClients(t)
	defer conn.Close()

	userID := "test-user-note-push"
	ctx := createDevModeContext(userID)

	// Begin session
	session, err := syncClient.BeginSession(ctx, &syncv1.BeginSessionRequest{})
	if err != nil {
		t.Fatalf("BeginSession failed: %v", err)
	}

	authCtx := createAuthenticatedContext(userID, session.Id, int(session.Epoch))

	tests := []struct {
		name      string
		item      map[string]interface{}
		wantError bool
		checkAck  func(*testing.T, *syncv1.PushAck)
	}{
		{
			name: "push valid note",
			item: map[string]interface{}{
				"uid":       "aaaa4444-0000-0000-0000-000000000000",
				"title":     "Test Note",
				"content":   "Test content",
				"updatedTs": "2025-11-09T10:00:00Z",
				"sync": map[string]interface{}{
					"version":   1,
					"isDeleted": false,
				},
			},
			wantError: false,
			checkAck: func(t *testing.T, ack *syncv1.PushAck) {
				if ack.Uid != "aaaa4444-0000-0000-0000-000000000000" {
					t.Errorf("Expected UID aaaa4444-0000-0000-0000-000000000000, got %s", ack.Uid)
				}
				if ack.Version != 1 {
					t.Errorf("Expected version 1, got %d", ack.Version)
				}
				if ack.Error != "" {
					t.Errorf("Expected no error, got: %s", ack.Error)
				}
			},
		},
		{
			name: "push update (LWW)",
			item: map[string]interface{}{
				"uid":       "aaaa4444-0000-0000-0000-000000000000",
				"title":     "Updated Note",
				"updatedTs": "2025-11-09T10:01:00Z", // Newer
				"sync": map[string]interface{}{
					"version": 1,
				},
			},
			wantError: false,
			checkAck: func(t *testing.T, ack *syncv1.PushAck) {
				if ack.Version != 2 {
					t.Errorf("Expected version incremented to 2, got %d", ack.Version)
				}
			},
		},
		{
			name: "push invalid (missing uid)",
			item: map[string]interface{}{
				"title":     "No UID",
				"updatedTs": "2025-11-09T10:00:00Z",
			},
			wantError: false, // Push doesn't fail, but ack contains error
			checkAck: func(t *testing.T, ack *syncv1.PushAck) {
				if ack.Error == "" {
					t.Error("Expected error for missing UID")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item, err := structpb.NewStruct(tt.item)
			if err != nil {
				t.Fatalf("Failed to create struct: %v", err)
			}

			resp, err := noteClient.Push(authCtx, &syncv1.PushRequest{Items: []*structpb.Struct{item}})
			if tt.wantError && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if err == nil && len(resp.Acks) > 0 && tt.checkAck != nil {
				tt.checkAck(t, resp.Acks[0])
			}
		})
	}
}

func TestNotePull(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	grpcServer := setupTestGrpcServer(t, pool)
	defer grpcServer.Stop()

	conn, syncClient, noteClient, _, _, _, _ := createTestClients(t)
	defer conn.Close()

	userID := "test-user-note-pull"
	ctx := createDevModeContext(userID)

	// Begin session
	session, err := syncClient.BeginSession(ctx, &syncv1.BeginSessionRequest{})
	if err != nil {
		t.Fatalf("BeginSession failed: %v", err)
	}

	authCtx := createAuthenticatedContext(userID, session.Id, int(session.Epoch))

	// Push some notes first
	notes := []*structpb.Struct{}
	noteUIDs := []string{
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
		"33333333-3333-3333-3333-333333333333",
	}
	for i, uid := range noteUIDs {
		item, _ := structpb.NewStruct(map[string]interface{}{
			"uid":       uid,
			"title":     fmt.Sprintf("Note %d", i+1),
			"updatedTs": "2025-11-09T10:00:00Z",
			"sync":      map[string]interface{}{"version": 1},
		})
		notes = append(notes, item)
	}

	_, err = noteClient.Push(authCtx, &syncv1.PushRequest{Items: notes})
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Pull all notes
	pullResp, err := noteClient.Pull(authCtx, &syncv1.PullRequest{Limit: 100})
	if err != nil {
		t.Fatalf("Pull failed: %v", err)
	}

	if len(pullResp.Upserts) != 3 {
		t.Errorf("Expected 3 notes, got %d", len(pullResp.Upserts))
	}

	if len(pullResp.Deletes) != 0 {
		t.Errorf("Expected 0 deletes, got %d", len(pullResp.Deletes))
	}

	// Pull with limit
	pullResp, err = noteClient.Pull(authCtx, &syncv1.PullRequest{Limit: 1})
	if err != nil {
		t.Fatalf("Pull with limit failed: %v", err)
	}

	if len(pullResp.Upserts) != 1 {
		t.Errorf("Expected 1 note with limit, got %d", len(pullResp.Upserts))
	}

	if pullResp.NextCursor == "" {
		t.Error("Expected next cursor for pagination")
	}
}

// ===== Entity RPC Tests - Tasks =====

func TestTaskPushAndPull(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	grpcServer := setupTestGrpcServer(t, pool)
	defer grpcServer.Stop()

	conn, syncClient, _, taskClient, _, _, _ := createTestClients(t)
	defer conn.Close()

	userID := "test-user-task"
	ctx := createDevModeContext(userID)

	// Begin session
	session, err := syncClient.BeginSession(ctx, &syncv1.BeginSessionRequest{})
	if err != nil {
		t.Fatalf("BeginSession failed: %v", err)
	}

	authCtx := createAuthenticatedContext(userID, session.Id, int(session.Epoch))

	// Push task
	taskItem, _ := structpb.NewStruct(map[string]interface{}{
		"uid":       "aaaa2222-0000-0000-0000-000000000000",
		"title":     "Test Task",
		"done":      false,
		"updatedTs": "2025-11-09T10:00:00Z",
		"sync":      map[string]interface{}{"version": 1},
	})

	pushResp, err := taskClient.Push(authCtx, &syncv1.PushRequest{Items: []*structpb.Struct{taskItem}})
	if err != nil {
		t.Fatalf("Task push failed: %v", err)
	}

	if len(pushResp.Acks) != 1 {
		t.Fatalf("Expected 1 ack, got %d", len(pushResp.Acks))
	}

	if pushResp.Acks[0].Error != "" {
		t.Errorf("Task push error: %s", pushResp.Acks[0].Error)
	}

	// Pull task
	pullResp, err := taskClient.Pull(authCtx, &syncv1.PullRequest{Limit: 100})
	if err != nil {
		t.Fatalf("Task pull failed: %v", err)
	}

	if len(pullResp.Upserts) != 1 {
		t.Errorf("Expected 1 task, got %d", len(pullResp.Upserts))
	}
}

// ===== Entity RPC Tests - Comments =====

func TestCommentPushAndPull(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	grpcServer := setupTestGrpcServer(t, pool)
	defer grpcServer.Stop()

	conn, syncClient, noteClient, _, commentClient, _, _ := createTestClients(t)
	defer conn.Close()

	userID := "test-user-comment"
	ctx := createDevModeContext(userID)

	// Begin session
	session, err := syncClient.BeginSession(ctx, &syncv1.BeginSessionRequest{})
	if err != nil {
		t.Fatalf("BeginSession failed: %v", err)
	}

	authCtx := createAuthenticatedContext(userID, session.Id, int(session.Epoch))

	// First, create a note (parent for comment)
	noteItem, _ := structpb.NewStruct(map[string]interface{}{
		"uid":       "eeee0000-0000-0000-0000-000000000000",
		"title":     "Parent Note",
		"updatedTs": "2025-11-09T10:00:00Z",
		"sync":      map[string]interface{}{"version": 1},
	})

	_, err = noteClient.Push(authCtx, &syncv1.PushRequest{Items: []*structpb.Struct{noteItem}})
	if err != nil {
		t.Fatalf("Note push failed: %v", err)
	}

	// Push comment
	commentItem, _ := structpb.NewStruct(map[string]interface{}{
		"uid":        "ffff0000-0000-0000-0000-000000000000",
		"content":    "Test Comment",
		"parentType": "note",
		"parentUid":  "eeee0000-0000-0000-0000-000000000000",
		"updatedTs":  "2025-11-09T10:01:00Z",
		"sync":       map[string]interface{}{"version": 1},
	})

	pushResp, err := commentClient.Push(authCtx, &syncv1.PushRequest{Items: []*structpb.Struct{commentItem}})
	if err != nil {
		t.Fatalf("Comment push failed: %v", err)
	}

	if len(pushResp.Acks) != 1 {
		t.Fatalf("Expected 1 ack, got %d", len(pushResp.Acks))
	}

	if pushResp.Acks[0].Error != "" {
		t.Errorf("Comment push error: %s", pushResp.Acks[0].Error)
	}

	// Pull comment
	pullResp, err := commentClient.Pull(authCtx, &syncv1.PullRequest{Limit: 100})
	if err != nil {
		t.Fatalf("Comment pull failed: %v", err)
	}

	if len(pullResp.Upserts) != 1 {
		t.Errorf("Expected 1 comment, got %d", len(pullResp.Upserts))
	}
}

// ===== Entity RPC Tests - Chats =====

func TestChatPushAndPull(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	grpcServer := setupTestGrpcServer(t, pool)
	defer grpcServer.Stop()

	conn, syncClient, _, _, _, chatClient, _ := createTestClients(t)
	defer conn.Close()

	userID := "test-user-chat"
	ctx := createDevModeContext(userID)

	// Begin session
	session, err := syncClient.BeginSession(ctx, &syncv1.BeginSessionRequest{})
	if err != nil {
		t.Fatalf("BeginSession failed: %v", err)
	}

	authCtx := createAuthenticatedContext(userID, session.Id, int(session.Epoch))

	// Push chat
	chatItem, _ := structpb.NewStruct(map[string]interface{}{
		"uid":       "aaaa3333-0000-0000-0000-000000000000",
		"title":     "Test Chat",
		"updatedTs": "2025-11-09T10:00:00Z",
		"sync":      map[string]interface{}{"version": 1},
	})

	pushResp, err := chatClient.Push(authCtx, &syncv1.PushRequest{Items: []*structpb.Struct{chatItem}})
	if err != nil {
		t.Fatalf("Chat push failed: %v", err)
	}

	if len(pushResp.Acks) != 1 {
		t.Fatalf("Expected 1 ack, got %d", len(pushResp.Acks))
	}

	if pushResp.Acks[0].Error != "" {
		t.Errorf("Chat push error: %s", pushResp.Acks[0].Error)
	}

	// Pull chat
	pullResp, err := chatClient.Pull(authCtx, &syncv1.PullRequest{Limit: 100})
	if err != nil {
		t.Fatalf("Chat pull failed: %v", err)
	}

	if len(pullResp.Upserts) != 1 {
		t.Errorf("Expected 1 chat, got %d", len(pullResp.Upserts))
	}
}

// ===== Entity RPC Tests - Chat Messages =====

func TestChatMessagePushAndPull(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	grpcServer := setupTestGrpcServer(t, pool)
	defer grpcServer.Stop()

	conn, syncClient, _, _, _, chatClient, chatMessageClient := createTestClients(t)
	defer conn.Close()

	userID := "test-user-chat-message"
	ctx := createDevModeContext(userID)

	// Begin session
	session, err := syncClient.BeginSession(ctx, &syncv1.BeginSessionRequest{})
	if err != nil {
		t.Fatalf("BeginSession failed: %v", err)
	}

	authCtx := createAuthenticatedContext(userID, session.Id, int(session.Epoch))

	// First, create a chat (parent for message)
	chatItem, _ := structpb.NewStruct(map[string]interface{}{
		"uid":       "aaaa1111-0000-0000-0000-000000000000",
		"title":     "Parent Chat",
		"updatedTs": "2025-11-09T10:00:00Z",
		"sync":      map[string]interface{}{"version": 1},
	})

	_, err = chatClient.Push(authCtx, &syncv1.PushRequest{Items: []*structpb.Struct{chatItem}})
	if err != nil {
		t.Fatalf("Chat push failed: %v", err)
	}

	// Push chat message
	messageItem, _ := structpb.NewStruct(map[string]interface{}{
		"uid":       "bbbb1111-0000-0000-0000-000000000000",
		"content":   "Test Message",
		"chatUid":   "aaaa1111-0000-0000-0000-000000000000",
		"role":      "user",
		"updatedTs": "2025-11-09T10:01:00Z",
		"sync":      map[string]interface{}{"version": 1},
	})

	pushResp, err := chatMessageClient.Push(authCtx, &syncv1.PushRequest{Items: []*structpb.Struct{messageItem}})
	if err != nil {
		t.Fatalf("Chat message push failed: %v", err)
	}

	if len(pushResp.Acks) != 1 {
		t.Fatalf("Expected 1 ack, got %d", len(pushResp.Acks))
	}

	if pushResp.Acks[0].Error != "" {
		t.Errorf("Chat message push error: %s", pushResp.Acks[0].Error)
	}

	// Pull chat message
	pullResp, err := chatMessageClient.Pull(authCtx, &syncv1.PullRequest{Limit: 100})
	if err != nil {
		t.Fatalf("Chat message pull failed: %v", err)
	}

	if len(pullResp.Upserts) != 1 {
		t.Errorf("Expected 1 chat message, got %d", len(pullResp.Upserts))
	}
}

// ===== Error Scenario Tests =====

func TestCommentPush_InvalidParent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	grpcServer := setupTestGrpcServer(t, pool)
	defer grpcServer.Stop()

	conn, syncClient, _, _, commentClient, _, _ := createTestClients(t)
	defer conn.Close()

	userID := "test-user-comment-invalid-parent"
	ctx := createDevModeContext(userID)

	// Begin session
	session, err := syncClient.BeginSession(ctx, &syncv1.BeginSessionRequest{})
	if err != nil {
		t.Fatalf("BeginSession failed: %v", err)
	}

	authCtx := createAuthenticatedContext(userID, session.Id, int(session.Epoch))

	// Try to push comment with non-existent parent
	commentItem, _ := structpb.NewStruct(map[string]interface{}{
		"uid":        "cccc1111-0000-0000-0000-000000000000",
		"content":    "Orphan Comment",
		"parentType": "note",
		"parentUid":  "99999999-9999-9999-9999-999999999999",
		"updatedTs":  "2025-11-09T10:00:00Z",
		"sync":       map[string]interface{}{"version": 1},
	})

	pushResp, err := commentClient.Push(authCtx, &syncv1.PushRequest{Items: []*structpb.Struct{commentItem}})
	if err != nil {
		t.Fatalf("Push should not fail at RPC level: %v", err)
	}

	// Should have error in ack
	if len(pushResp.Acks) != 1 {
		t.Fatalf("Expected 1 ack, got %d", len(pushResp.Acks))
	}

	if pushResp.Acks[0].Error == "" {
		t.Error("Expected error for non-existent parent")
	}

	if !contains(pushResp.Acks[0].Error, "parent") {
		t.Errorf("Expected parent-related error, got: %s", pushResp.Acks[0].Error)
	}
}

func TestChatMessagePush_InvalidChat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	grpcServer := setupTestGrpcServer(t, pool)
	defer grpcServer.Stop()

	conn, syncClient, _, _, _, _, chatMessageClient := createTestClients(t)
	defer conn.Close()

	userID := "test-user-message-invalid-chat"
	ctx := createDevModeContext(userID)

	// Begin session
	session, err := syncClient.BeginSession(ctx, &syncv1.BeginSessionRequest{})
	if err != nil {
		t.Fatalf("BeginSession failed: %v", err)
	}

	authCtx := createAuthenticatedContext(userID, session.Id, int(session.Epoch))

	// Try to push message with non-existent chat
	messageItem, _ := structpb.NewStruct(map[string]interface{}{
		"uid":       "dddd1111-0000-0000-0000-000000000000",
		"content":   "Orphan Message",
		"chatUid":   "88888888-8888-8888-8888-888888888888",
		"role":      "user",
		"updatedTs": "2025-11-09T10:00:00Z",
		"sync":      map[string]interface{}{"version": 1},
	})

	pushResp, err := chatMessageClient.Push(authCtx, &syncv1.PushRequest{Items: []*structpb.Struct{messageItem}})
	if err != nil {
		t.Fatalf("Push should not fail at RPC level: %v", err)
	}

	// Should have error in ack
	if len(pushResp.Acks) != 1 {
		t.Fatalf("Expected 1 ack, got %d", len(pushResp.Acks))
	}

	if pushResp.Acks[0].Error == "" {
		t.Error("Expected error for non-existent chat")
	}
}

// ===== Concurrency Tests =====

func TestConcurrentPush(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	grpcServer := setupTestGrpcServer(t, pool)
	defer grpcServer.Stop()

	conn, syncClient, noteClient, _, _, _, _ := createTestClients(t)
	defer conn.Close()

	userID := "test-user-concurrent"
	ctx := createDevModeContext(userID)

	// Begin session
	session, err := syncClient.BeginSession(ctx, &syncv1.BeginSessionRequest{})
	if err != nil {
		t.Fatalf("BeginSession failed: %v", err)
	}

	authCtx := createAuthenticatedContext(userID, session.Id, int(session.Epoch))

	// Push multiple notes concurrently
	done := make(chan bool)
	uids := []string{
		"aaaa0000-0000-0000-0000-000000000000",
		"bbbb0000-0000-0000-0000-000000000000",
		"cccc0000-0000-0000-0000-000000000000",
		"dddd0000-0000-0000-0000-000000000000",
		"eeee0000-0000-0000-0000-000000000000",
	}
	for i := 0; i < 5; i++ {
		go func(index int) {
			noteItem, _ := structpb.NewStruct(map[string]interface{}{
				"uid":       uids[index],
				"title":     fmt.Sprintf("Concurrent Note %d", index),
				"updatedTs": "2025-11-09T10:00:00Z",
				"sync":      map[string]interface{}{"version": 1},
			})

			_, err := noteClient.Push(authCtx, &syncv1.PushRequest{Items: []*structpb.Struct{noteItem}})
			if err != nil {
				t.Errorf("Concurrent push %d failed: %v", index, err)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 5; i++ {
		<-done
	}

	// Verify all notes were created
	pullResp, err := noteClient.Pull(authCtx, &syncv1.PullRequest{Limit: 100})
	if err != nil {
		t.Fatalf("Pull after concurrent push failed: %v", err)
	}

	if len(pullResp.Upserts) != 5 {
		t.Errorf("Expected 5 notes after concurrent push, got %d", len(pullResp.Upserts))
	}
}

// Helper functions
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsSubstring(s, substr)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
