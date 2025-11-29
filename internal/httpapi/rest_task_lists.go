package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/erauner12/toolbridge-api/internal/service/syncservice"
	"github.com/erauner12/toolbridge-api/internal/syncx"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// ============================================================================
// Task Lists REST Handlers
// ============================================================================

// ListTaskLists handles GET /v1/task_lists
func (s *Server) ListTaskLists(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	limit := parseLimit(r.URL.Query().Get("limit"), 500, 1000)
	cur, ok := syncx.DecodeCursor(r.URL.Query().Get("cursor"))
	if !ok {
		cur = syncx.Cursor{Ms: 0, UID: uuid.Nil}
	}
	includeDeleted := parseIncludeDeleted(r)

	resp, err := s.TaskListSvc.ListTaskLists(ctx, userID, cur, limit, includeDeleted)
	if err != nil {
		logger.Error().Err(err).Msg("failed to list task_lists")
		writeError(w, r, 500, "failed to list task_lists")
		return
	}

	writeJSON(w, 200, resp)
}

// CreateTaskList handles POST /v1/task_lists
func (s *Server) CreateTaskList(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	item, err := s.TaskListSvc.ApplyTaskListMutation(ctx, userID, payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to create task_list")
		writeError(w, r, 500, "failed to create task_list")
		return
	}

	writeJSON(w, 201, item)
}

// GetTaskList handles GET /v1/task_lists/{uid}
func (s *Server) GetTaskList(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	includeDeleted := parseIncludeDeleted(r)
	item, err := s.TaskListSvc.GetTaskList(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get task_list")
		writeError(w, r, 500, "failed to get task_list")
		return
	}

	if item == nil {
		writeError(w, r, 404, "task_list not found")
		return
	}

	if item.DeletedAt != nil && !includeDeleted {
		writeJSON(w, 410, map[string]any{
			"error":     "task_list deleted",
			"deletedAt": item.DeletedAt,
		})
		return
	}

	writeJSON(w, 200, item)
}

// UpdateTaskList handles PUT /v1/task_lists/{uid}
func (s *Server) UpdateTaskList(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	existing, err := s.TaskListSvc.GetTaskList(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get task_list for update")
		writeError(w, r, 500, "failed to get task_list")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "task_list not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "task_list deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	payload["uid"] = uid.String()

	opts := syncservice.MutationOpts{}
	usedIfMatch := false
	if version, ok := parseIfMatchHeader(r); ok {
		opts.EnforceVersion = true
		opts.ExpectedVersion = version
		usedIfMatch = true
	}

	item, err := s.TaskListSvc.ApplyTaskListMutation(ctx, userID, payload, opts)
	if err != nil {
		if _, ok := err.(*syncservice.VersionMismatchError); ok {
			statusCode := 412
			if !usedIfMatch {
				statusCode = 409
			}
			writeError(w, r, statusCode, "version mismatch: "+err.Error())
			return
		}
		logger.Error().Err(err).Msg("failed to update task_list")
		writeError(w, r, 500, "failed to update task_list")
		return
	}

	writeJSON(w, 200, item)
}

// PatchTaskList handles PATCH /v1/task_lists/{uid}
func (s *Server) PatchTaskList(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	existing, err := s.TaskListSvc.GetTaskList(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get task_list for patch")
		writeError(w, r, 500, "failed to get task_list")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "task_list not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "task_list deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	var partial map[string]any
	if err := json.NewDecoder(r.Body).Decode(&partial); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	merged := existing.Payload
	for k, v := range partial {
		if k != "uid" && k != "sync" {
			merged[k] = v
		}
	}

	opts := syncservice.MutationOpts{}
	usedIfMatch := false
	if version, ok := parseIfMatchHeader(r); ok {
		opts.EnforceVersion = true
		opts.ExpectedVersion = version
		usedIfMatch = true
	}

	item, err := s.TaskListSvc.ApplyTaskListMutation(ctx, userID, merged, opts)
	if err != nil {
		if _, ok := err.(*syncservice.VersionMismatchError); ok {
			statusCode := 412
			if !usedIfMatch {
				statusCode = 409
			}
			writeError(w, r, statusCode, "version mismatch: "+err.Error())
			return
		}
		logger.Error().Err(err).Msg("failed to patch task_list")
		writeError(w, r, 500, "failed to patch task_list")
		return
	}

	writeJSON(w, 200, item)
}

// DeleteTaskList handles DELETE /v1/task_lists/{uid}
// Tasks in the list are orphaned (taskListUid set to null) atomically with the deletion
func (s *Server) DeleteTaskList(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	existing, err := s.TaskListSvc.GetTaskList(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get task_list for delete")
		writeError(w, r, 500, "failed to get task_list")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "task_list not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "task_list already deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Atomically orphan tasks and soft-delete the task list
	// Both operations succeed or fail together
	result, err := s.TaskListSvc.DeleteTaskListWithOrphan(ctx, userID, uid, existing.Payload)
	if err != nil {
		logger.Error().Err(err).Msg("failed to delete task_list")
		writeError(w, r, 500, "failed to delete task_list")
		return
	}

	logger.Info().
		Str("task_list_uid", uid.String()).
		Int64("orphaned_tasks", result.OrphanedCount).
		Msg("deleted task list and orphaned tasks")

	writeJSON(w, 200, result.Item)
}

// ArchiveTaskList handles POST /v1/task_lists/{uid}/archive
func (s *Server) ArchiveTaskList(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	existing, err := s.TaskListSvc.GetTaskList(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get task_list for archive")
		writeError(w, r, 500, "failed to get task_list")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "task_list not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "task_list deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	payload := existing.Payload
	payload["archived"] = true

	item, err := s.TaskListSvc.ApplyTaskListMutation(ctx, userID, payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to archive task_list")
		writeError(w, r, 500, "failed to archive task_list")
		return
	}

	writeJSON(w, 200, item)
}

// ProcessTaskList handles POST /v1/task_lists/{uid}/process
func (s *Server) ProcessTaskList(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	existing, err := s.TaskListSvc.GetTaskList(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get task_list for process")
		writeError(w, r, 500, "failed to get task_list")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "task_list not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "task_list deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	var req struct {
		Action   string         `json:"action"`
		Metadata map[string]any `json:"metadata,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Process actions for task lists
	switch req.Action {
	case "unarchive":
		existing.Payload["archived"] = false
	default:
		writeError(w, r, 400, "unknown action: "+req.Action)
		return
	}

	item, err := s.TaskListSvc.ApplyTaskListMutation(ctx, userID, existing.Payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to process task_list")
		writeError(w, r, 500, "failed to process task_list")
		return
	}

	writeJSON(w, 200, item)
}

// ============================================================================
// Task List Categories REST Handlers
// ============================================================================

// ListTaskListCategories handles GET /v1/task_list_categories
func (s *Server) ListTaskListCategories(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	limit := parseLimit(r.URL.Query().Get("limit"), 500, 1000)
	cur, ok := syncx.DecodeCursor(r.URL.Query().Get("cursor"))
	if !ok {
		cur = syncx.Cursor{Ms: 0, UID: uuid.Nil}
	}
	includeDeleted := parseIncludeDeleted(r)

	resp, err := s.TaskListCategorySvc.ListTaskListCategories(ctx, userID, cur, limit, includeDeleted)
	if err != nil {
		logger.Error().Err(err).Msg("failed to list task_list_categories")
		writeError(w, r, 500, "failed to list task_list_categories")
		return
	}

	writeJSON(w, 200, resp)
}

// CreateTaskListCategory handles POST /v1/task_list_categories
func (s *Server) CreateTaskListCategory(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	item, err := s.TaskListCategorySvc.ApplyTaskListCategoryMutation(ctx, userID, payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to create task_list_category")
		writeError(w, r, 500, "failed to create task_list_category")
		return
	}

	writeJSON(w, 201, item)
}

// GetTaskListCategory handles GET /v1/task_list_categories/{uid}
func (s *Server) GetTaskListCategory(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	includeDeleted := parseIncludeDeleted(r)
	item, err := s.TaskListCategorySvc.GetTaskListCategory(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get task_list_category")
		writeError(w, r, 500, "failed to get task_list_category")
		return
	}

	if item == nil {
		writeError(w, r, 404, "task_list_category not found")
		return
	}

	if item.DeletedAt != nil && !includeDeleted {
		writeJSON(w, 410, map[string]any{
			"error":     "task_list_category deleted",
			"deletedAt": item.DeletedAt,
		})
		return
	}

	writeJSON(w, 200, item)
}

// UpdateTaskListCategory handles PUT /v1/task_list_categories/{uid}
func (s *Server) UpdateTaskListCategory(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	existing, err := s.TaskListCategorySvc.GetTaskListCategory(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get task_list_category for update")
		writeError(w, r, 500, "failed to get task_list_category")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "task_list_category not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "task_list_category deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	payload["uid"] = uid.String()

	opts := syncservice.MutationOpts{}
	usedIfMatch := false
	if version, ok := parseIfMatchHeader(r); ok {
		opts.EnforceVersion = true
		opts.ExpectedVersion = version
		usedIfMatch = true
	}

	item, err := s.TaskListCategorySvc.ApplyTaskListCategoryMutation(ctx, userID, payload, opts)
	if err != nil {
		if _, ok := err.(*syncservice.VersionMismatchError); ok {
			statusCode := 412
			if !usedIfMatch {
				statusCode = 409
			}
			writeError(w, r, statusCode, "version mismatch: "+err.Error())
			return
		}
		logger.Error().Err(err).Msg("failed to update task_list_category")
		writeError(w, r, 500, "failed to update task_list_category")
		return
	}

	writeJSON(w, 200, item)
}

// PatchTaskListCategory handles PATCH /v1/task_list_categories/{uid}
func (s *Server) PatchTaskListCategory(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	existing, err := s.TaskListCategorySvc.GetTaskListCategory(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get task_list_category for patch")
		writeError(w, r, 500, "failed to get task_list_category")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "task_list_category not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "task_list_category deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	var partial map[string]any
	if err := json.NewDecoder(r.Body).Decode(&partial); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	merged := existing.Payload
	for k, v := range partial {
		if k != "uid" && k != "sync" {
			merged[k] = v
		}
	}

	opts := syncservice.MutationOpts{}
	usedIfMatch := false
	if version, ok := parseIfMatchHeader(r); ok {
		opts.EnforceVersion = true
		opts.ExpectedVersion = version
		usedIfMatch = true
	}

	item, err := s.TaskListCategorySvc.ApplyTaskListCategoryMutation(ctx, userID, merged, opts)
	if err != nil {
		if _, ok := err.(*syncservice.VersionMismatchError); ok {
			statusCode := 412
			if !usedIfMatch {
				statusCode = 409
			}
			writeError(w, r, statusCode, "version mismatch: "+err.Error())
			return
		}
		logger.Error().Err(err).Msg("failed to patch task_list_category")
		writeError(w, r, 500, "failed to patch task_list_category")
		return
	}

	writeJSON(w, 200, item)
}

// DeleteTaskListCategory handles DELETE /v1/task_list_categories/{uid}
func (s *Server) DeleteTaskListCategory(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	existing, err := s.TaskListCategorySvc.GetTaskListCategory(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get task_list_category for delete")
		writeError(w, r, 500, "failed to get task_list_category")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "task_list_category not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "task_list_category already deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	opts := syncservice.MutationOpts{SetDeleted: true}
	item, err := s.TaskListCategorySvc.ApplyTaskListCategoryMutation(ctx, userID, existing.Payload, opts)
	if err != nil {
		logger.Error().Err(err).Msg("failed to delete task_list_category")
		writeError(w, r, 500, "failed to delete task_list_category")
		return
	}

	writeJSON(w, 200, item)
}

// ArchiveTaskListCategory handles POST /v1/task_list_categories/{uid}/archive
func (s *Server) ArchiveTaskListCategory(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	existing, err := s.TaskListCategorySvc.GetTaskListCategory(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get task_list_category for archive")
		writeError(w, r, 500, "failed to get task_list_category")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "task_list_category not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "task_list_category deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	payload := existing.Payload
	payload["archived"] = true

	item, err := s.TaskListCategorySvc.ApplyTaskListCategoryMutation(ctx, userID, payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to archive task_list_category")
		writeError(w, r, 500, "failed to archive task_list_category")
		return
	}

	writeJSON(w, 200, item)
}

// ProcessTaskListCategory handles POST /v1/task_list_categories/{uid}/process
func (s *Server) ProcessTaskListCategory(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	existing, err := s.TaskListCategorySvc.GetTaskListCategory(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get task_list_category for process")
		writeError(w, r, 500, "failed to get task_list_category")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "task_list_category not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "task_list_category deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	var req struct {
		Action   string         `json:"action"`
		Metadata map[string]any `json:"metadata,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	switch req.Action {
	case "unarchive":
		existing.Payload["archived"] = false
	default:
		writeError(w, r, 400, "unknown action: "+req.Action)
		return
	}

	item, err := s.TaskListCategorySvc.ApplyTaskListCategoryMutation(ctx, userID, existing.Payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to process task_list_category")
		writeError(w, r, 500, "failed to process task_list_category")
		return
	}

	writeJSON(w, 200, item)
}
