package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/erauner12/toolbridge-api/internal/syncx"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// PushChats handles POST /v1/sync/chats/push
// Implements Last-Write-Wins (LWW) conflict resolution with idempotent pushes
func (s *Server) PushChats(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	// Use contextual logger with correlation ID
	logger := log.Ctx(ctx)

	logger.Info().Str("user_id", userID).Str("entity_type", "chats").Msg("sync_push_started")

	var req pushReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Warn().Err(err).Msg("invalid push request body")
		writeJSON(w, 400, []pushAck{{Error: "invalid json"}})
		return
	}

	acks := make([]pushAck, 0, len(req.Items))

	// Use transaction for atomicity (all-or-nothing per batch)
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("failed to begin transaction")
		writeJSON(w, 500, []pushAck{{Error: "transaction error"}})
		return
	}
	defer tx.Rollback(ctx)

	for _, item := range req.Items {
		// Call the refactored service layer
		svcAck := s.ChatSvc.PushChatItem(ctx, tx, userID, item)

		// Convert service PushAck to HTTP pushAck
		acks = append(acks, pushAck{
			UID:       svcAck.UID,
			Version:   svcAck.Version,
			UpdatedAt: svcAck.UpdatedAt,
			Error:     svcAck.Error,
		})
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error().Err(err).Msg("failed to commit transaction")
		writeJSON(w, 500, []pushAck{{Error: "commit failed"}})
		return
	}

	logger.Info().
		Str("user_id", userID).
		Int("success_count", len(acks)).
		Msg("sync_push_completed: chats")

	writeJSON(w, 200, acks)
}

// PullChats handles GET /v1/sync/chats/pull?cursor=<opaque>&limit=<int>
// Returns upserts and deletes in deterministic order using cursor-based pagination
func (s *Server) PullChats(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	// Use contextual logger with correlation ID
	logger := log.Ctx(ctx)

	// Parse query params
	limit := parseLimit(r.URL.Query().Get("limit"), 500, 1000)
	cur, ok := syncx.DecodeCursor(r.URL.Query().Get("cursor"))
	if !ok {
		// No cursor = start from beginning (epoch)
		cur = syncx.Cursor{Ms: 0, UID: uuid.Nil}
	}

	logger.Info().
		Str("user_id", userID).
		Int("limit", limit).
		Str("cursor", r.URL.Query().Get("cursor")).
		Msg("sync_pull_started: chats")

	// Call the refactored service layer
	resp, err := s.ChatSvc.PullChats(ctx, userID, cur, limit)
	if err != nil {
		writeError(w, r, 500, "pull failed")
		return
	}

	logger.Info().
		Str("user_id", userID).
		Int("upsert_count", len(resp.Upserts)).
		Int("delete_count", len(resp.Deletes)).
		Bool("has_next_page", resp.NextCursor != nil).
		Msg("sync_pull_completed: chats")

	writeJSON(w, 200, pullResp{
		Upserts:    resp.Upserts,
		Deletes:    resp.Deletes,
		NextCursor: resp.NextCursor,
	})
}
