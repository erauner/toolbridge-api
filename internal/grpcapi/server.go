package grpcapi

import (
	"context"
	"time"

	syncv1 "github.com/erauner12/toolbridge-api/gen/go/sync/v1"
	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/erauner12/toolbridge-api/internal/service/syncservice"
	"github.com/erauner12/toolbridge-api/internal/syncx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Server implements all gRPC sync services
type Server struct {
	// Embed unimplemented servers for forward compatibility
	syncv1.UnimplementedSyncServiceServer
	syncv1.UnimplementedNoteSyncServiceServer
	syncv1.UnimplementedTaskSyncServiceServer
	syncv1.UnimplementedCommentSyncServiceServer
	syncv1.UnimplementedChatSyncServiceServer
	syncv1.UnimplementedChatMessageSyncServiceServer

	// Dependencies
	DB      *pgxpool.Pool
	NoteSvc *syncservice.NoteService
	// TODO: Add other services when implemented
	// TaskSvc    *syncservice.TaskService
	// CommentSvc *syncservice.CommentService
	// ChatSvc    *syncservice.ChatService
	// ChatMessageSvc *syncservice.ChatMessageService
}

// NewServer creates a new gRPC server instance
func NewServer(db *pgxpool.Pool, noteSvc *syncservice.NoteService) *Server {
	return &Server{
		DB:      db,
		NoteSvc: noteSvc,
	}
}

// ===================================================================
// NoteSyncService Implementation (Phase 1: Unary/Batch RPCs)
// ===================================================================

// Push implements NoteSyncService.Push
func (s *Server) Push(ctx context.Context, req *syncv1.PushRequest) (*syncv1.PushResponse, error) {
	logger := log.Ctx(ctx)

	// 1. Get userID from context (set by auth interceptor)
	userID := auth.UserID(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing user")
	}

	logger.Info().
		Str("user_id", userID).
		Int("item_count", len(req.Items)).
		Msg("grpc_notes_push_started")

	// 2. Begin transaction
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("failed to begin transaction")
		return nil, status.Error(codes.Internal, "db error")
	}
	defer tx.Rollback(ctx)

	acks := make([]*syncv1.PushAck, 0, len(req.Items))

	// 3. Loop through items and call service
	for _, itemStruct := range req.Items {
		// Convert proto Struct to map[string]any
		itemMap := itemStruct.AsMap()

		// 4. Call shared business logic
		svcAck := s.NoteSvc.PushNoteItem(ctx, tx, userID, itemMap)

		// 5. Convert service PushAck to proto
		protoAck := &syncv1.PushAck{
			Uid:     svcAck.UID,
			Version: int32(svcAck.Version),
			Error:   svcAck.Error,
		}

		// Parse UpdatedAt timestamp
		if ms, ok := syncx.ParseTimeToMs(svcAck.UpdatedAt); ok {
			protoAck.UpdatedAt = timestamppb.New(syncx.MsToTime(ms))
		}

		acks = append(acks, protoAck)
	}

	// 6. Commit transaction
	if err := tx.Commit(ctx); err != nil {
		logger.Error().Err(err).Msg("failed to commit transaction")
		return nil, status.Error(codes.Internal, "commit error")
	}

	logger.Info().
		Str("user_id", userID).
		Int("success_count", len(acks)).
		Msg("grpc_notes_push_completed")

	return &syncv1.PushResponse{Acks: acks}, nil
}

// Pull implements NoteSyncService.Pull
func (s *Server) Pull(ctx context.Context, req *syncv1.PullRequest) (*syncv1.PullResponse, error) {
	logger := log.Ctx(ctx)

	// 1. Get userID from context (set by auth interceptor)
	userID := auth.UserID(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing user")
	}

	// 2. Parse cursor and limit
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 500 // default
	}
	if limit > 1000 {
		limit = 1000 // max
	}

	cur := syncx.Cursor{Ms: 0, UID: uuid.Nil}
	if req.Cursor != "" {
		if decoded, ok := syncx.DecodeCursor(req.Cursor); ok {
			cur = decoded
		}
	}

	logger.Info().
		Str("user_id", userID).
		Int("limit", limit).
		Str("cursor", req.Cursor).
		Msg("grpc_notes_pull_started")

	// 3. Call service
	resp, err := s.NoteSvc.PullNotes(ctx, userID, cur, limit)
	if err != nil {
		logger.Error().Err(err).Msg("failed to pull notes")
		return nil, status.Error(codes.Internal, "pull failed")
	}

	// 4. Convert response to proto
	upserts := make([]*structpb.Struct, 0, len(resp.Upserts))
	for _, item := range resp.Upserts {
		st, err := structpb.NewStruct(item)
		if err != nil {
			logger.Warn().Err(err).Msg("failed to convert upsert to proto struct")
			continue
		}
		upserts = append(upserts, st)
	}

	deletes := make([]*structpb.Struct, 0, len(resp.Deletes))
	for _, item := range resp.Deletes {
		st, err := structpb.NewStruct(item)
		if err != nil {
			logger.Warn().Err(err).Msg("failed to convert delete to proto struct")
			continue
		}
		deletes = append(deletes, st)
	}

	protoResp := &syncv1.PullResponse{
		Upserts: upserts,
		Deletes: deletes,
	}
	if resp.NextCursor != nil {
		protoResp.NextCursor = *resp.NextCursor
	}

	logger.Info().
		Str("user_id", userID).
		Int("upsert_count", len(upserts)).
		Int("delete_count", len(deletes)).
		Bool("has_next_page", resp.NextCursor != nil).
		Msg("grpc_notes_pull_completed")

	return protoResp, nil
}

// ===================================================================
// Core SyncService Implementation (Sessions, Info, Wipe)
// ===================================================================

// GetServerInfo implements SyncService.GetServerInfo
func (s *Server) GetServerInfo(ctx context.Context, req *syncv1.GetServerInfoRequest) (*syncv1.ServerInfo, error) {
	// TODO: Implement server info (reuse logic from httpapi/info.go)
	return nil, status.Error(codes.Unimplemented, "not yet implemented")
}

// BeginSession implements SyncService.BeginSession
func (s *Server) BeginSession(ctx context.Context, req *syncv1.BeginSessionRequest) (*syncv1.SyncSession, error) {
	// TODO: Implement session creation (reuse logic from httpapi/sessions.go)
	return nil, status.Error(codes.Unimplemented, "not yet implemented")
}

// EndSession implements SyncService.EndSession
func (s *Server) EndSession(ctx context.Context, req *syncv1.EndSessionRequest) (*syncv1.EndSessionResponse, error) {
	// TODO: Implement session termination
	return nil, status.Error(codes.Unimplemented, "not yet implemented")
}

// WipeAccount implements SyncService.WipeAccount
func (s *Server) WipeAccount(ctx context.Context, req *syncv1.WipeAccountRequest) (*syncv1.WipeResult, error) {
	// TODO: Implement account wipe
	return nil, status.Error(codes.Unimplemented, "not yet implemented")
}

// GetSyncState implements SyncService.GetSyncState
func (s *Server) GetSyncState(ctx context.Context, req *syncv1.GetSyncStateRequest) (*syncv1.UserSyncState, error) {
	// TODO: Implement sync state retrieval
	return nil, status.Error(codes.Unimplemented, "not yet implemented")
}
