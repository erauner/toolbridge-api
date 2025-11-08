# gRPC Migration Implementation Guide - Phase 1

This document tracks the implementation of Phase 1 of the gRPC migration, establishing gRPC transport in parallel with the existing REST API.

## ✅ Completed

### 1. Protocol Buffers Definition
- **File**: `proto/sync/v1/sync.proto`
- **What**: Defined all sync service interfaces using unary (batch-oriented) RPCs
- **Services**:
  - `SyncService` - Core service (sessions, info, wipe, state)
  - `NoteSyncService`, `TaskSyncService`, `CommentSyncService`, `ChatSyncService`, `ChatMessageSyncService`
- **Messages**: Request/Response types using `google.protobuf.Struct` for Phase 1 flexibility

### 2. Shared Session Store
- **Files**:
  - `internal/session/store.go` - Shared in-memory session store
  - `internal/httpapi/sessions.go` - Updated to use shared store
  - `internal/httpapi/session_required.go` - Updated to use shared store
- **What**: Refactored session management out of HTTP package so both HTTP and gRPC can share it
- **Interface**: `CreateSession`, `GetSession`, `DeleteSession`, `DeleteUserSessions`

### 3. Service Layer (Business Logic Extraction)
- **Package**: `internal/service/syncservice/`
- **Implemented**:
  - `notes_service.go` - Extracted notes push/pull logic
    - `PushNoteItem(ctx, tx, userID, item)` - LWW upsert for single note
    - `PullNotes(ctx, userID, cursor, limit)` - Cursor-based pagination
- **Pattern**: Service layer is transport-agnostic, returns simple structs (`PushAck`, `PullResponse`)

### 4. HTTP Handlers Refactored
- **File**: `internal/httpapi/sync_notes.go`
- **What**: HTTP handlers now call service layer instead of embedding business logic
- **Benefit**: Business logic can be reused by gRPC handlers

### 5. gRPC Server Implementation (Partial)
- **File**: `internal/grpcapi/server.go`
- **Implemented**:
  - `NoteSyncService.Push` - Demonstrates pattern for converting proto → service → proto
  - `NoteSyncService.Pull` - Demonstrates cursor handling and proto conversion
- **Pattern Established**:
  ```go
  1. Extract userID from context (set by auth interceptor)
  2. Begin transaction (for push)
  3. Loop through items, call service layer
  4. Convert service response to proto
  5. Commit transaction
  6. Return proto response
  ```

### 6. Build Infrastructure
- **Files**:
  - `scripts/generate_proto.sh` - Script to generate Go protobuf stubs
  - `proto/README.md` - Documentation for installing protoc and generating code
- **Next Step**: Install `protoc` and run `./scripts/generate_proto.sh` to generate stubs

---

## 🚧 TODO: Complete Backend Implementation

### 7. Generate Protobuf Stubs
```bash
# Install prerequisites (macOS)
brew install protobuf
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Generate code
./scripts/generate_proto.sh
```

This generates:
- `gen/go/sync/v1/sync.pb.go` - Message types
- `gen/go/sync/v1/sync_grpc.pb.go` - Service interfaces

### 8. Create Remaining Service Layer Files

Follow the pattern from `notes_service.go` to create:

#### `internal/service/syncservice/tasks_service.go`
```go
package syncservice

type TaskService struct {
    DB *pgxpool.Pool
}

func NewTaskService(db *pgxpool.Pool) *TaskService {
    return &TaskService{DB: db}
}

func (s *TaskService) PushTaskItem(ctx context.Context, tx pgx.Tx, userID string, item map[string]any) PushAck {
    // Copy logic from internal/httpapi/sync_tasks.go PushTasks loop body
}

func (s *TaskService) PullTasks(ctx context.Context, userID string, cursor syncx.Cursor, limit int) (*PullResponse, error) {
    // Copy logic from internal/httpapi/sync_tasks.go PullTasks
}
```

#### `internal/service/syncservice/comments_service.go`
```go
type CommentService struct {
    DB *pgxpool.Pool
}

func (s *CommentService) PushCommentItem(ctx context.Context, tx pgx.Tx, userID string, item map[string]any) PushAck {
    // IMPORTANT: Include parent validation logic from sync_comments.go
    // Comments require checking that parent (note/task) exists
}

func (s *CommentService) PullComments(ctx context.Context, userID string, cursor syncx.Cursor, limit int) (*PullResponse, error) {
    // ...
}
```

#### `internal/service/syncservice/chats_service.go`
```go
type ChatService struct {
    DB *pgxpool.Pool
}

func (s *ChatService) PushChatItem(ctx context.Context, tx pgx.Tx, userID string, item map[string]any) PushAck {
    // ...
}

func (s *ChatService) PullChats(ctx context.Context, userID string, cursor syncx.Cursor, limit int) (*PullResponse, error) {
    // ...
}
```

#### `internal/service/syncservice/chat_messages_service.go`
```go
type ChatMessageService struct {
    DB *pgxpool.Pool
}

func (s *ChatMessageService) PushChatMessageItem(ctx context.Context, tx pgx.Tx, userID string, item map[string]any) PushAck {
    // IMPORTANT: Include parent chat validation from sync_chat_messages.go
}

func (s *ChatMessageService) PullChatMessages(ctx context.Context, userID string, cursor syncx.Cursor, limit int) (*PullResponse, error) {
    // ...
}
```

### 9. Update HTTP Handlers to Use Services

For each entity (`sync_tasks.go`, `sync_comments.go`, `sync_chats.go`, `sync_chat_messages.go`):

```go
// Before (inline logic):
for _, item := range req.Items {
    ext, err := syncx.ExtractCommon(item)
    // ... 50 lines of SQL and logic ...
}

// After (call service):
for _, item := range req.Items {
    svcAck := s.TaskSvc.PushTaskItem(ctx, tx, userID, item)
    acks = append(acks, pushAck{
        UID:       svcAck.UID,
        Version:   svcAck.Version,
        UpdatedAt: svcAck.UpdatedAt,
        Error:     svcAck.Error,
    })
}
```

### 10. Wire Services in `main.go` and `router.go`

**`internal/httpapi/router.go`:**
```go
type Server struct {
    DB              *pgxpool.Pool
    RateLimitConfig RateLimitInfo
    // Services
    NoteSvc        *syncservice.NoteService
    TaskSvc        *syncservice.TaskService
    CommentSvc     *syncservice.CommentService
    ChatSvc        *syncservice.ChatService
    ChatMessageSvc *syncservice.ChatMessageService
}
```

**`cmd/server/main.go`:**
```go
srv := &httpapi.Server{
    DB:              pool,
    RateLimitConfig: httpapi.DefaultRateLimitConfig,
    NoteSvc:         syncservice.NewNoteService(pool),
    TaskSvc:         syncservice.NewTaskService(pool),
    CommentSvc:      syncservice.NewCommentService(pool),
    ChatSvc:         syncservice.NewChatService(pool),
    ChatMessageSvc:  syncservice.NewChatMessageService(pool),
}
```

### 11. Create gRPC Interceptors

**File**: `internal/grpcapi/interceptors.go`

```go
package grpcapi

import (
    "context"
    "github.com/erauner12/toolbridge-api/internal/auth"
    "github.com/erauner12/toolbridge-api/internal/session"
    "github.com/google/uuid"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/metadata"
    "google.golang.org/grpc/status"
)

// AuthInterceptor validates JWT and sets userID in context (mirrors auth.Middleware)
func AuthInterceptor(db *pgxpool.Pool, cfg auth.JWTCfg) grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
        // 1. Read "authorization" from metadata
        md, ok := metadata.FromIncomingContext(ctx)
        if !ok {
            return nil, status.Error(codes.Unauthenticated, "missing metadata")
        }

        authHeaders := md.Get("authorization")
        if len(authHeaders) == 0 {
            // Check if DevMode allows X-Debug-Sub
            debugSub := md.Get("x-debug-sub")
            if cfg.DevMode && len(debugSub) > 0 {
                // Allow debug mode (same as HTTP middleware)
                userID := debugSub[0]
                ctx = context.WithValue(ctx, auth.CtxUserID, userID)
                return handler(ctx, req)
            }
            return nil, status.Error(codes.Unauthenticated, "missing authorization header")
        }

        // 2. Parse and validate token (reuse auth/jwt.go logic)
        // ... (extract token, validate using cfg.HS256Secret or Auth0 JWKS) ...

        // 3. Find or create app_user (reuse logic from auth.Middleware)
        // ... (query/insert app_user table) ...

        // 4. Add userID to context
        ctx = context.WithValue(ctx, auth.CtxUserID, userID)
        return handler(ctx, req)
    }
}

// SessionInterceptor validates X-Sync-Session header (mirrors SessionRequired)
func SessionInterceptor() grpc.UnaryServerInterceptor {
    sessionStore := session.GetStore()

    return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
        // Skip session check for certain RPCs (BeginSession, GetServerInfo, etc.)
        if isSessionExempt(info.FullMethod) {
            return handler(ctx, req)
        }

        // 1. Read X-Sync-Session from metadata
        md, _ := metadata.FromIncomingContext(ctx)
        sessionHeaders := md.Get("x-sync-session")
        if len(sessionHeaders) == 0 {
            return nil, status.Error(codes.FailedPrecondition, "X-Sync-Session header required")
        }
        sessionID := sessionHeaders[0]

        // 2. Validate session
        sess, ok := sessionStore.GetSession(sessionID)
        if !ok {
            return nil, status.Error(codes.FailedPrecondition, "Invalid or expired session")
        }

        // 3. Verify session belongs to authenticated user
        userID := auth.UserID(ctx)
        if sess.UserID != userID {
            return nil, status.Error(codes.PermissionDenied, "Session does not belong to user")
        }

        return handler(ctx, req)
    }
}

// EpochInterceptor validates X-Sync-Epoch (mirrors EpochRequired)
func EpochInterceptor(db *pgxpool.Pool) grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
        // Skip epoch check for certain RPCs
        if isEpochExempt(info.FullMethod) {
            return handler(ctx, req)
        }

        // 1. Read X-Sync-Epoch from metadata
        md, _ := metadata.FromIncomingContext(ctx)
        epochHeaders := md.Get("x-sync-epoch")
        if len(epochHeaders) == 0 {
            return nil, status.Error(codes.FailedPrecondition, "X-Sync-Epoch header required")
        }
        clientEpoch, _ := strconv.Atoi(epochHeaders[0])

        // 2. Query server epoch
        userID := auth.UserID(ctx)
        var serverEpoch int
        err := db.QueryRow(ctx, `SELECT epoch FROM owner_state WHERE owner_id = $1`, userID).Scan(&serverEpoch)
        if err != nil {
            return nil, status.Error(codes.Internal, "Failed to load epoch")
        }

        // 3. Check mismatch
        if clientEpoch != serverEpoch {
            // Return error with epoch in trailer (gRPC equivalent of response header)
            // Client must detect this and trigger reset
            return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Epoch mismatch: server=%d", serverEpoch))
        }

        return handler(ctx, req)
    }
}

// CorrelationIDInterceptor generates or reads correlation ID (mirrors CorrelationMiddleware)
func CorrelationIDInterceptor() grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
        md, _ := metadata.FromIncomingContext(ctx)
        corrHeaders := md.Get("x-correlation-id")

        var corrID string
        if len(corrHeaders) > 0 {
            corrID = corrHeaders[0]
        } else {
            corrID = uuid.New().String()
        }

        // Add to context for logging
        ctx = context.WithValue(ctx, "correlationId", corrID)

        // Add to zerolog context
        logger := log.With().Str("correlation_id", corrID).Logger()
        ctx = logger.WithContext(ctx)

        return handler(ctx, req)
    }
}

// Helper functions
func isSessionExempt(method string) bool {
    exempt := []string{
        "/toolbridge.sync.v1.SyncService/GetServerInfo",
        "/toolbridge.sync.v1.SyncService/BeginSession",
    }
    for _, e := range exempt {
        if method == e {
            return true
        }
    }
    return false
}

func isEpochExempt(method string) bool {
    // Same as session exempt, plus EndSession
    return isSessionExempt(method) || method == "/toolbridge.sync.v1.SyncService/EndSession"
}
```

### 12. Complete gRPC Server Implementation

**`internal/grpcapi/server.go`** - Implement remaining services:

```go
// Add to Server struct
type Server struct {
    // ... existing ...
    TaskSvc        *syncservice.TaskService
    CommentSvc     *syncservice.CommentService
    ChatSvc        *syncservice.ChatService
    ChatMessageSvc *syncservice.ChatMessageService
}

// TaskSyncService.Push (copy pattern from NoteSyncService.Push)
func (s *Server) Push(ctx context.Context, req *syncv1.PushRequest) (*syncv1.PushResponse, error) {
    // Same pattern as notes, but call s.TaskSvc.PushTaskItem
}

// TaskSyncService.Pull (copy pattern from NoteSyncService.Pull)
func (s *Server) Pull(ctx context.Context, req *syncv1.PullRequest) (*syncv1.PullResponse, error) {
    // Same pattern as notes, but call s.TaskSvc.PullTasks
}

// Repeat for CommentSyncService, ChatSyncService, ChatMessageSyncService
```

### 13. Update `main.go` to Start gRPC Server

**`cmd/server/main.go`:**

```go
import (
    "net"
    "github.com/erauner12/toolbridge-api/internal/grpcapi"
    syncv1 "github.com/erauner12/toolbridge-api/gen/go/sync/v1"
    "google.golang.org/grpc"
    "google.golang.org/grpc/reflection"
)

func main() {
    // ... existing HTTP setup ...

    // === NEW gRPC SERVER SETUP ===
    grpcAddr := env("GRPC_ADDR", ":8082")
    lis, err := net.Listen("tcp", grpcAddr)
    if err != nil {
        log.Fatal().Err(err).Msg("failed to listen for gRPC")
    }

    // Chain interceptors
    grpcServer := grpc.NewServer(
        grpc.ChainUnaryInterceptor(
            grpcapi.CorrelationIDInterceptor(),
            grpcapi.AuthInterceptor(pool, jwtCfg),
            grpcapi.SessionInterceptor(),
            grpcapi.EpochInterceptor(pool),
        ),
    )

    // Create and register gRPC implementation
    grpcApiServer := grpcapi.NewServer(
        pool,
        srv.NoteSvc,
        // TODO: Pass other services
    )
    syncv1.RegisterSyncServiceServer(grpcServer, grpcApiServer)
    syncv1.RegisterNoteSyncServiceServer(grpcServer, grpcApiServer)
    syncv1.RegisterTaskSyncServiceServer(grpcServer, grpcApiServer)
    syncv1.RegisterCommentSyncServiceServer(grpcServer, grpcApiServer)
    syncv1.RegisterChatSyncServiceServer(grpcServer, grpcApiServer)
    syncv1.RegisterChatMessageSyncServiceServer(grpcServer, grpcApiServer)

    reflection.Register(grpcServer) // Enable gRPC reflection for testing

    // Start gRPC server in goroutine
    go func() {
        log.Info().Str("addr", grpcAddr).Msg("starting gRPC server")
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatal().Err(err).Msg("gRPC server failed")
        }
    }()

    // === GRACEFUL SHUTDOWN ===
    // ... existing signal handling ...
    <-sigChan
    log.Info().Msg("shutting down gracefully...")

    // Stop gRPC server
    go func() {
        grpcServer.GracefulStop()
    }()

    // Stop HTTP server
    // ... existing httpServer.Shutdown() ...
}
```

---

## 🎯 Testing Backend

Once backend is complete:

```bash
# Start server
DATABASE_URL=... ENV=dev go run cmd/server/main.go

# Test with grpcurl (requires reflection)
grpcurl -plaintext localhost:8082 list
grpcurl -plaintext localhost:8082 list toolbridge.sync.v1.SyncService
grpcurl -plaintext -d '{}' localhost:8082 toolbridge.sync.v1.SyncService/GetServerInfo
```

---

## 📱 Next: Flutter Client Implementation

See `FLUTTER_GRPC_SETUP.md` for:
1. Generating Dart protobuf stubs
2. Implementing `GrpcSyncApi`
3. Adding settings toggle
4. Updating `remote_sync_provider.dart`

---

## 🔑 Key Design Decisions

1. **Service Layer Pattern**: Business logic extracted to `syncservice` package
   - Keeps HTTP and gRPC handlers thin (just transport concerns)
   - Makes testing easier (test service layer directly)
   - Enables future transports (WebSocket, etc.)

2. **Shared Session Store**: In-memory store accessible to both transports
   - Alternative: Could use Redis for multi-instance deployment
   - Current design sufficient for single-instance Phase 1

3. **Proto `Struct` Usage**: Phase 1 uses `google.protobuf.Struct` for flexibility
   - Matches existing `List<Map<String, dynamic>>` pattern
   - Phase 2 can migrate to typed messages for better performance

4. **Interceptor Chain**: gRPC interceptors mirror HTTP middleware stack
   - Ensures consistent auth/session/epoch behavior
   - Reuses validation logic from `auth` and `session` packages
