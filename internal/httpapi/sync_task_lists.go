package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/erauner12/toolbridge-api/internal/syncx"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// ============================================================================
// Task Lists Sync Handlers
// ============================================================================

// PushTaskLists handles POST /v1/sync/task_lists/push
func (s *Server) PushTaskLists(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	logger.Info().Str("user_id", userID).Str("entity_type", "task_lists").Msg("sync_push_started")

	var req pushReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Warn().Err(err).Msg("invalid push request body")
		writeJSON(w, 400, []pushAck{{Error: "invalid json"}})
		return
	}

	acks := make([]pushAck, 0, len(req.Items))

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("failed to begin transaction")
		writeJSON(w, 500, []pushAck{{Error: "transaction error"}})
		return
	}
	defer tx.Rollback(ctx)

	for _, item := range req.Items {
		svcAck := s.TaskListSvc.PushTaskListItem(ctx, tx, userID, item)
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
		Msg("sync_push_completed: task_lists")

	writeJSON(w, 200, acks)
}

// PullTaskLists handles GET /v1/sync/task_lists/pull
func (s *Server) PullTaskLists(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	limit := parseLimit(r.URL.Query().Get("limit"), 500, 1000)
	cur, ok := syncx.DecodeCursor(r.URL.Query().Get("cursor"))
	if !ok {
		cur = syncx.Cursor{Ms: 0, UID: uuid.Nil}
	}

	logger.Info().
		Str("user_id", userID).
		Int("limit", limit).
		Str("cursor", r.URL.Query().Get("cursor")).
		Msg("sync_pull_started: task_lists")

	resp, err := s.TaskListSvc.PullTaskLists(ctx, userID, cur, limit)
	if err != nil {
		writeError(w, r, 500, "pull failed")
		return
	}

	logger.Info().
		Str("user_id", userID).
		Int("upsert_count", len(resp.Upserts)).
		Int("delete_count", len(resp.Deletes)).
		Bool("has_next_page", resp.NextCursor != nil).
		Msg("sync_pull_completed: task_lists")

	writeJSON(w, 200, pullResp{
		Upserts:    resp.Upserts,
		Deletes:    resp.Deletes,
		NextCursor: resp.NextCursor,
	})
}

// ============================================================================
// Task List Categories Sync Handlers
// ============================================================================

// PushTaskListCategories handles POST /v1/sync/task_list_categories/push
func (s *Server) PushTaskListCategories(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	logger.Info().Str("user_id", userID).Str("entity_type", "task_list_categories").Msg("sync_push_started")

	var req pushReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Warn().Err(err).Msg("invalid push request body")
		writeJSON(w, 400, []pushAck{{Error: "invalid json"}})
		return
	}

	acks := make([]pushAck, 0, len(req.Items))

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("failed to begin transaction")
		writeJSON(w, 500, []pushAck{{Error: "transaction error"}})
		return
	}
	defer tx.Rollback(ctx)

	for _, item := range req.Items {
		svcAck := s.TaskListCategorySvc.PushTaskListCategoryItem(ctx, tx, userID, item)
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
		Msg("sync_push_completed: task_list_categories")

	writeJSON(w, 200, acks)
}

// PullTaskListCategories handles GET /v1/sync/task_list_categories/pull
func (s *Server) PullTaskListCategories(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	limit := parseLimit(r.URL.Query().Get("limit"), 500, 1000)
	cur, ok := syncx.DecodeCursor(r.URL.Query().Get("cursor"))
	if !ok {
		cur = syncx.Cursor{Ms: 0, UID: uuid.Nil}
	}

	logger.Info().
		Str("user_id", userID).
		Int("limit", limit).
		Str("cursor", r.URL.Query().Get("cursor")).
		Msg("sync_pull_started: task_list_categories")

	resp, err := s.TaskListCategorySvc.PullTaskListCategories(ctx, userID, cur, limit)
	if err != nil {
		writeError(w, r, 500, "pull failed")
		return
	}

	logger.Info().
		Str("user_id", userID).
		Int("upsert_count", len(resp.Upserts)).
		Int("delete_count", len(resp.Deletes)).
		Bool("has_next_page", resp.NextCursor != nil).
		Msg("sync_pull_completed: task_list_categories")

	writeJSON(w, 200, pullResp{
		Upserts:    resp.Upserts,
		Deletes:    resp.Deletes,
		NextCursor: resp.NextCursor,
	})
}
