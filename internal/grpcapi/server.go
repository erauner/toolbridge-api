//go:build grpc
// +build grpc

package grpcapi

import (
	"context"
	"database/sql"

	syncv1 "github.com/erauner12/toolbridge-api/gen/go/sync/v1"
	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/erauner12/toolbridge-api/internal/service/syncservice"
	"github.com/erauner12/toolbridge-api/internal/session"
	"github.com/erauner12/toolbridge-api/internal/syncx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	DB             *pgxpool.Pool
	NoteSvc        *syncservice.NoteService
	TaskSvc        *syncservice.TaskService
	CommentSvc     *syncservice.CommentService
	ChatSvc        *syncservice.ChatService
	ChatMessageSvc *syncservice.ChatMessageService
}

// NewServer creates a new gRPC server instance
func NewServer(
	db *pgxpool.Pool,
	noteSvc *syncservice.NoteService,
	taskSvc *syncservice.TaskService,
	commentSvc *syncservice.CommentService,
	chatSvc *syncservice.ChatService,
	chatMessageSvc *syncservice.ChatMessageService,
) *Server {
	return &Server{
		DB:             db,
		NoteSvc:        noteSvc,
		TaskSvc:        taskSvc,
		CommentSvc:     commentSvc,
		ChatSvc:        chatSvc,
		ChatMessageSvc: chatMessageSvc,
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
// TaskSyncService Wrapper
// ===================================================================

// TaskServer wraps the main Server to implement TaskSyncServiceServer
type TaskServer struct {
	syncv1.UnimplementedTaskSyncServiceServer
	*Server
}

// Push implements TaskSyncService.Push
func (ts *TaskServer) Push(ctx context.Context, req *syncv1.PushRequest) (*syncv1.PushResponse, error) {
	logger := log.Ctx(ctx)
	userID := auth.UserID(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing user")
	}

	logger.Info().Str("user_id", userID).Int("item_count", len(req.Items)).Msg("grpc_tasks_push_started")

	tx, err := ts.DB.Begin(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("failed to begin transaction")
		return nil, status.Error(codes.Internal, "db error")
	}
	defer tx.Rollback(ctx)

	acks := make([]*syncv1.PushAck, 0, len(req.Items))
	for _, itemStruct := range req.Items {
		itemMap := itemStruct.AsMap()
		svcAck := ts.TaskSvc.PushTaskItem(ctx, tx, userID, itemMap)

		protoAck := &syncv1.PushAck{
			Uid:     svcAck.UID,
			Version: int32(svcAck.Version),
			Error:   svcAck.Error,
		}
		if ms, ok := syncx.ParseTimeToMs(svcAck.UpdatedAt); ok {
			protoAck.UpdatedAt = timestamppb.New(syncx.MsToTime(ms))
		}
		acks = append(acks, protoAck)
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error().Err(err).Msg("failed to commit transaction")
		return nil, status.Error(codes.Internal, "commit error")
	}

	logger.Info().Str("user_id", userID).Int("success_count", len(acks)).Msg("grpc_tasks_push_completed")
	return &syncv1.PushResponse{Acks: acks}, nil
}

// Pull implements TaskSyncService.Pull
func (ts *TaskServer) Pull(ctx context.Context, req *syncv1.PullRequest) (*syncv1.PullResponse, error) {
	logger := log.Ctx(ctx)
	userID := auth.UserID(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing user")
	}

	limit := int(req.Limit)
	if limit <= 0 {
		limit = 500
	}
	if limit > 1000 {
		limit = 1000
	}

	cur := syncx.Cursor{Ms: 0, UID: uuid.Nil}
	if req.Cursor != "" {
		if decoded, ok := syncx.DecodeCursor(req.Cursor); ok {
			cur = decoded
		}
	}

	logger.Info().Str("user_id", userID).Int("limit", limit).Str("cursor", req.Cursor).Msg("grpc_tasks_pull_started")

	resp, err := ts.TaskSvc.PullTasks(ctx, userID, cur, limit)
	if err != nil {
		logger.Error().Err(err).Msg("failed to pull tasks")
		return nil, status.Error(codes.Internal, "pull failed")
	}

	upserts := make([]*structpb.Struct, 0, len(resp.Upserts))
	for _, item := range resp.Upserts {
		if st, err := structpb.NewStruct(item); err == nil {
			upserts = append(upserts, st)
		}
	}

	deletes := make([]*structpb.Struct, 0, len(resp.Deletes))
	for _, item := range resp.Deletes {
		if st, err := structpb.NewStruct(item); err == nil {
			deletes = append(deletes, st)
		}
	}

	protoResp := &syncv1.PullResponse{Upserts: upserts, Deletes: deletes}
	if resp.NextCursor != nil {
		protoResp.NextCursor = *resp.NextCursor
	}

	logger.Info().Str("user_id", userID).Int("upsert_count", len(upserts)).Int("delete_count", len(deletes)).Msg("grpc_tasks_pull_completed")
	return protoResp, nil
}

// ===================================================================
// CommentSyncService Wrapper
// ===================================================================

// CommentServer wraps the main Server to implement CommentSyncServiceServer
type CommentServer struct {
	syncv1.UnimplementedCommentSyncServiceServer
	*Server
}

// Push implements CommentSyncService.Push
func (cs *CommentServer) Push(ctx context.Context, req *syncv1.PushRequest) (*syncv1.PushResponse, error) {
	logger := log.Ctx(ctx)
	userID := auth.UserID(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing user")
	}

	logger.Info().Str("user_id", userID).Int("item_count", len(req.Items)).Msg("grpc_comments_push_started")

	tx, err := cs.DB.Begin(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("failed to begin transaction")
		return nil, status.Error(codes.Internal, "db error")
	}
	defer tx.Rollback(ctx)

	acks := make([]*syncv1.PushAck, 0, len(req.Items))
	for _, itemStruct := range req.Items {
		itemMap := itemStruct.AsMap()
		svcAck := cs.CommentSvc.PushCommentItem(ctx, tx, userID, itemMap)

		protoAck := &syncv1.PushAck{
			Uid:     svcAck.UID,
			Version: int32(svcAck.Version),
			Error:   svcAck.Error,
		}
		if ms, ok := syncx.ParseTimeToMs(svcAck.UpdatedAt); ok {
			protoAck.UpdatedAt = timestamppb.New(syncx.MsToTime(ms))
		}
		acks = append(acks, protoAck)
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error().Err(err).Msg("failed to commit transaction")
		return nil, status.Error(codes.Internal, "commit error")
	}

	logger.Info().Str("user_id", userID).Int("success_count", len(acks)).Msg("grpc_comments_push_completed")
	return &syncv1.PushResponse{Acks: acks}, nil
}

// Pull implements CommentSyncService.Pull
func (cs *CommentServer) Pull(ctx context.Context, req *syncv1.PullRequest) (*syncv1.PullResponse, error) {
	logger := log.Ctx(ctx)
	userID := auth.UserID(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing user")
	}

	limit := int(req.Limit)
	if limit <= 0 {
		limit = 500
	}
	if limit > 1000 {
		limit = 1000
	}

	cur := syncx.Cursor{Ms: 0, UID: uuid.Nil}
	if req.Cursor != "" {
		if decoded, ok := syncx.DecodeCursor(req.Cursor); ok {
			cur = decoded
		}
	}

	logger.Info().Str("user_id", userID).Int("limit", limit).Str("cursor", req.Cursor).Msg("grpc_comments_pull_started")

	resp, err := cs.CommentSvc.PullComments(ctx, userID, cur, limit)
	if err != nil {
		logger.Error().Err(err).Msg("failed to pull comments")
		return nil, status.Error(codes.Internal, "pull failed")
	}

	upserts := make([]*structpb.Struct, 0, len(resp.Upserts))
	for _, item := range resp.Upserts {
		if st, err := structpb.NewStruct(item); err == nil {
			upserts = append(upserts, st)
		}
	}

	deletes := make([]*structpb.Struct, 0, len(resp.Deletes))
	for _, item := range resp.Deletes {
		if st, err := structpb.NewStruct(item); err == nil {
			deletes = append(deletes, st)
		}
	}

	protoResp := &syncv1.PullResponse{Upserts: upserts, Deletes: deletes}
	if resp.NextCursor != nil {
		protoResp.NextCursor = *resp.NextCursor
	}

	logger.Info().Str("user_id", userID).Int("upsert_count", len(upserts)).Int("delete_count", len(deletes)).Msg("grpc_comments_pull_completed")
	return protoResp, nil
}

// ===================================================================
// ChatSyncService Wrapper
// ===================================================================

// ChatServer wraps the main Server to implement ChatSyncServiceServer
type ChatServer struct {
	syncv1.UnimplementedChatSyncServiceServer
	*Server
}

// Push implements ChatSyncService.Push
func (chs *ChatServer) Push(ctx context.Context, req *syncv1.PushRequest) (*syncv1.PushResponse, error) {
	logger := log.Ctx(ctx)
	userID := auth.UserID(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing user")
	}

	logger.Info().Str("user_id", userID).Int("item_count", len(req.Items)).Msg("grpc_chats_push_started")

	tx, err := chs.DB.Begin(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("failed to begin transaction")
		return nil, status.Error(codes.Internal, "db error")
	}
	defer tx.Rollback(ctx)

	acks := make([]*syncv1.PushAck, 0, len(req.Items))
	for _, itemStruct := range req.Items {
		itemMap := itemStruct.AsMap()
		svcAck := chs.ChatSvc.PushChatItem(ctx, tx, userID, itemMap)

		protoAck := &syncv1.PushAck{
			Uid:     svcAck.UID,
			Version: int32(svcAck.Version),
			Error:   svcAck.Error,
		}
		if ms, ok := syncx.ParseTimeToMs(svcAck.UpdatedAt); ok {
			protoAck.UpdatedAt = timestamppb.New(syncx.MsToTime(ms))
		}
		acks = append(acks, protoAck)
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error().Err(err).Msg("failed to commit transaction")
		return nil, status.Error(codes.Internal, "commit error")
	}

	logger.Info().Str("user_id", userID).Int("success_count", len(acks)).Msg("grpc_chats_push_completed")
	return &syncv1.PushResponse{Acks: acks}, nil
}

// Pull implements ChatSyncService.Pull
func (chs *ChatServer) Pull(ctx context.Context, req *syncv1.PullRequest) (*syncv1.PullResponse, error) {
	logger := log.Ctx(ctx)
	userID := auth.UserID(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing user")
	}

	limit := int(req.Limit)
	if limit <= 0 {
		limit = 500
	}
	if limit > 1000 {
		limit = 1000
	}

	cur := syncx.Cursor{Ms: 0, UID: uuid.Nil}
	if req.Cursor != "" {
		if decoded, ok := syncx.DecodeCursor(req.Cursor); ok {
			cur = decoded
		}
	}

	logger.Info().Str("user_id", userID).Int("limit", limit).Str("cursor", req.Cursor).Msg("grpc_chats_pull_started")

	resp, err := chs.ChatSvc.PullChats(ctx, userID, cur, limit)
	if err != nil {
		logger.Error().Err(err).Msg("failed to pull chats")
		return nil, status.Error(codes.Internal, "pull failed")
	}

	upserts := make([]*structpb.Struct, 0, len(resp.Upserts))
	for _, item := range resp.Upserts {
		if st, err := structpb.NewStruct(item); err == nil {
			upserts = append(upserts, st)
		}
	}

	deletes := make([]*structpb.Struct, 0, len(resp.Deletes))
	for _, item := range resp.Deletes {
		if st, err := structpb.NewStruct(item); err == nil {
			deletes = append(deletes, st)
		}
	}

	protoResp := &syncv1.PullResponse{Upserts: upserts, Deletes: deletes}
	if resp.NextCursor != nil {
		protoResp.NextCursor = *resp.NextCursor
	}

	logger.Info().Str("user_id", userID).Int("upsert_count", len(upserts)).Int("delete_count", len(deletes)).Msg("grpc_chats_pull_completed")
	return protoResp, nil
}

// ===================================================================
// ChatMessageSyncService Wrapper
// ===================================================================

// ChatMessageServer wraps the main Server to implement ChatMessageSyncServiceServer
type ChatMessageServer struct {
	syncv1.UnimplementedChatMessageSyncServiceServer
	*Server
}

// Push implements ChatMessageSyncService.Push
func (cms *ChatMessageServer) Push(ctx context.Context, req *syncv1.PushRequest) (*syncv1.PushResponse, error) {
	logger := log.Ctx(ctx)
	userID := auth.UserID(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing user")
	}

	logger.Info().Str("user_id", userID).Int("item_count", len(req.Items)).Msg("grpc_chat_messages_push_started")

	tx, err := cms.DB.Begin(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("failed to begin transaction")
		return nil, status.Error(codes.Internal, "db error")
	}
	defer tx.Rollback(ctx)

	acks := make([]*syncv1.PushAck, 0, len(req.Items))
	for _, itemStruct := range req.Items {
		itemMap := itemStruct.AsMap()
		svcAck := cms.ChatMessageSvc.PushChatMessageItem(ctx, tx, userID, itemMap)

		protoAck := &syncv1.PushAck{
			Uid:     svcAck.UID,
			Version: int32(svcAck.Version),
			Error:   svcAck.Error,
		}
		if ms, ok := syncx.ParseTimeToMs(svcAck.UpdatedAt); ok {
			protoAck.UpdatedAt = timestamppb.New(syncx.MsToTime(ms))
		}
		acks = append(acks, protoAck)
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error().Err(err).Msg("failed to commit transaction")
		return nil, status.Error(codes.Internal, "commit error")
	}

	logger.Info().Str("user_id", userID).Int("success_count", len(acks)).Msg("grpc_chat_messages_push_completed")
	return &syncv1.PushResponse{Acks: acks}, nil
}

// Pull implements ChatMessageSyncService.Pull
func (cms *ChatMessageServer) Pull(ctx context.Context, req *syncv1.PullRequest) (*syncv1.PullResponse, error) {
	logger := log.Ctx(ctx)
	userID := auth.UserID(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing user")
	}

	limit := int(req.Limit)
	if limit <= 0 {
		limit = 500
	}
	if limit > 1000 {
		limit = 1000
	}

	cur := syncx.Cursor{Ms: 0, UID: uuid.Nil}
	if req.Cursor != "" {
		if decoded, ok := syncx.DecodeCursor(req.Cursor); ok {
			cur = decoded
		}
	}

	logger.Info().Str("user_id", userID).Int("limit", limit).Str("cursor", req.Cursor).Msg("grpc_chat_messages_pull_started")

	resp, err := cms.ChatMessageSvc.PullChatMessages(ctx, userID, cur, limit)
	if err != nil {
		logger.Error().Err(err).Msg("failed to pull chat_messages")
		return nil, status.Error(codes.Internal, "pull failed")
	}

	upserts := make([]*structpb.Struct, 0, len(resp.Upserts))
	for _, item := range resp.Upserts {
		if st, err := structpb.NewStruct(item); err == nil {
			upserts = append(upserts, st)
		}
	}

	deletes := make([]*structpb.Struct, 0, len(resp.Deletes))
	for _, item := range resp.Deletes {
		if st, err := structpb.NewStruct(item); err == nil {
			deletes = append(deletes, st)
		}
	}

	protoResp := &syncv1.PullResponse{Upserts: upserts, Deletes: deletes}
	if resp.NextCursor != nil {
		protoResp.NextCursor = *resp.NextCursor
	}

	logger.Info().Str("user_id", userID).Int("upsert_count", len(upserts)).Int("delete_count", len(deletes)).Msg("grpc_chat_messages_pull_completed")
	return protoResp, nil
}

// ===================================================================
// Core SyncService Implementation (Sessions, Info, Wipe)
// ===================================================================

// GetServerInfo implements SyncService.GetServerInfo
// Returns server capabilities, API version, and supported features
func (s *Server) GetServerInfo(ctx context.Context, req *syncv1.GetServerInfoRequest) (*syncv1.ServerInfo, error) {
	logger := log.Ctx(ctx)
	logger.Debug().Msg("GetServerInfo called")

	return &syncv1.ServerInfo{
		ApiVersion: "1.1",
		ServerTime: timestamppb.Now(),
		Entities: map[string]*syncv1.EntityCapability{
			"notes": {
				MaxLimit: 1000,
				Push:     true,
				Pull:     true,
			},
			"tasks": {
				MaxLimit: 1000,
				Push:     true,
				Pull:     true,
			},
			"comments": {
				MaxLimit: 1000,
				Push:     true,
				Pull:     true,
			},
			"chats": {
				MaxLimit: 1000,
				Push:     true,
				Pull:     true,
			},
			"chat_messages": {
				MaxLimit: 1000,
				Push:     true,
				Pull:     true,
			},
		},
		Locking: &syncv1.LockingCapability{
			Supported: true,
			Mode:      "session",
		},
		MinClientVersion: "0.1.0",
		RateLimit: &syncv1.RateLimitInfo{
			WindowSeconds: 60,
			MaxRequests:   5,
			Burst:         2,
		},
		Hints: &syncv1.SyncHints{
			RecommendedBatch: 500,
			BackoffMsOn_429:  1500,
		},
	}, nil
}

// BeginSession implements SyncService.BeginSession
// Creates a new sync session for the authenticated user
func (s *Server) BeginSession(ctx context.Context, req *syncv1.BeginSessionRequest) (*syncv1.SyncSession, error) {
	logger := log.Ctx(ctx)
	userID := auth.UserID(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}

	// Load or create owner_state row (lazy initialization)
	var epoch int
	err := s.DB.QueryRow(ctx, `
		INSERT INTO owner_state(owner_id, epoch, created_at, updated_at)
		VALUES ($1, 1, NOW(), NOW())
		ON CONFLICT (owner_id) DO NOTHING
		RETURNING epoch
	`, userID).Scan(&epoch)

	if err != nil {
		// If insert did nothing (row exists), select epoch
		if err == pgx.ErrNoRows {
			err = s.DB.QueryRow(ctx,
				`SELECT epoch FROM owner_state WHERE owner_id = $1`,
				userID,
			).Scan(&epoch)
			if err != nil {
				logger.Error().Err(err).Str("userId", userID).Msg("Failed to load epoch")
				return nil, status.Error(codes.Internal, "Failed to load epoch")
			}
		} else {
			logger.Error().Err(err).Str("userId", userID).Msg("Failed to initialize epoch")
			return nil, status.Error(codes.Internal, "Failed to initialize epoch")
		}
	}

	// Create session with epoch using shared session store
	sessionStore := session.GetStore()
	sess := sessionStore.CreateSession(userID, epoch)

	logger.Info().
		Str("sessionId", sess.ID).
		Str("userId", userID).
		Int("epoch", epoch).
		Time("expiresAt", sess.ExpiresAt).
		Msg("sync session created")

	// Convert to protobuf message
	return &syncv1.SyncSession{
		Id:        sess.ID,
		UserId:    sess.UserID,
		CreatedAt: timestamppb.New(sess.CreatedAt),
		ExpiresAt: timestamppb.New(sess.ExpiresAt),
		Epoch:     int32(sess.Epoch),
	}, nil
}

// EndSession implements SyncService.EndSession
// Ends an active sync session
func (s *Server) EndSession(ctx context.Context, req *syncv1.EndSessionRequest) (*syncv1.EndSessionResponse, error) {
	logger := log.Ctx(ctx)
	userID := auth.UserID(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}

	sessionID := req.GetSessionId()
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session ID required")
	}

	// Verify session belongs to user
	sessionStore := session.GetStore()
	sess, exists := sessionStore.GetSession(sessionID)
	if !exists {
		return nil, status.Error(codes.NotFound, "session not found or expired")
	}

	if sess.UserID != userID {
		return nil, status.Error(codes.PermissionDenied, "forbidden")
	}

	sessionStore.DeleteSession(sessionID)

	logger.Info().
		Str("sessionId", sessionID).
		Str("userId", userID).
		Msg("sync session ended")

	return &syncv1.EndSessionResponse{}, nil
}

// WipeAccount implements SyncService.WipeAccount
// Permanently deletes all synced data for the authenticated user
func (s *Server) WipeAccount(ctx context.Context, req *syncv1.WipeAccountRequest) (*syncv1.WipeResult, error) {
	logger := log.Ctx(ctx)
	userID := auth.UserID(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}

	// Require explicit confirmation
	if req.GetConfirm() != "WIPE" {
		return nil, status.Error(codes.InvalidArgument, "confirmation required: must send confirm=\"WIPE\"")
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		logger.Error().Err(err).Str("userId", userID).Msg("Failed to begin transaction")
		return nil, status.Error(codes.Internal, "transaction begin failed")
	}
	defer tx.Rollback(ctx)

	// Bump epoch (atomically)
	var newEpoch int
	err = tx.QueryRow(ctx, `
		INSERT INTO owner_state(owner_id, epoch, last_wipe_at, last_wipe_by, created_at, updated_at)
		VALUES ($1, 2, NOW(), $1, NOW(), NOW())
		ON CONFLICT (owner_id) DO UPDATE
			SET epoch = owner_state.epoch + 1,
				last_wipe_at = NOW(),
				last_wipe_by = EXCLUDED.last_wipe_by,
				updated_at = NOW()
		RETURNING epoch
	`, userID).Scan(&newEpoch)

	if err != nil {
		logger.Error().Err(err).Str("userId", userID).Msg("Failed to bump epoch")
		return nil, status.Error(codes.Internal, "epoch update failed")
	}

	// Delete all entity rows for this user
	deleted := make(map[string]int32)
	tables := []string{"chat_message", "comment", "chat", "task", "note"}

	for _, table := range tables {
		var count int
		err := tx.QueryRow(ctx, `
			WITH del AS (
				DELETE FROM `+table+` WHERE owner_id = $1 RETURNING 1
			)
			SELECT COUNT(*) FROM del
		`, userID).Scan(&count)

		if err != nil {
			logger.Error().Err(err).Str("table", table).Str("userId", userID).Msg("Failed to delete rows")
			return nil, status.Error(codes.Internal, "delete failed: "+table)
		}
		deleted[table] = int32(count)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		logger.Error().Err(err).Str("userId", userID).Msg("Failed to commit wipe transaction")
		return nil, status.Error(codes.Internal, "commit failed")
	}

	// Invalidate all sessions for this user (outside transaction)
	sessionStore := session.GetStore()
	sessionsDeleted := sessionStore.DeleteUserSessions(userID)

	logger.Info().
		Str("userId", userID).
		Int("newEpoch", newEpoch).
		Interface("deleted", deleted).
		Int("sessionsInvalidated", sessionsDeleted).
		Msg("Account wiped successfully")

	return &syncv1.WipeResult{
		Epoch:   int32(newEpoch),
		Deleted: deleted,
	}, nil
}

// GetSyncState implements SyncService.GetSyncState
// Returns the current sync state for the authenticated user
func (s *Server) GetSyncState(ctx context.Context, req *syncv1.GetSyncStateRequest) (*syncv1.UserSyncState, error) {
	logger := log.Ctx(ctx)
	userID := auth.UserID(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}

	var epoch int
	var lastWipeAt sql.NullTime
	var lastWipeBy sql.NullString

	err := s.DB.QueryRow(ctx, `
		SELECT epoch, last_wipe_at, last_wipe_by
		FROM owner_state
		WHERE owner_id = $1
	`, userID).Scan(&epoch, &lastWipeAt, &lastWipeBy)

	if err != nil {
		// If row doesn't exist, return default state (epoch=1)
		if err == pgx.ErrNoRows {
			return &syncv1.UserSyncState{
				Epoch: 1,
			}, nil
		}

		logger.Error().Err(err).Str("userId", userID).Msg("Failed to load sync state")
		return nil, status.Error(codes.Internal, "failed to load sync state")
	}

	resp := &syncv1.UserSyncState{
		Epoch: int32(epoch),
	}

	if lastWipeAt.Valid {
		resp.LastWipeAt = timestamppb.New(lastWipeAt.Time)
	}
	if lastWipeBy.Valid {
		resp.LastWipeBy = lastWipeBy.String
	}

	return resp, nil
}
