package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/erauner12/toolbridge-api/internal/service/syncservice"
	"github.com/erauner12/toolbridge-api/internal/syncx"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// ============================================================================
// REST CRUD Handlers for Entity Management
// ============================================================================
//
// This file implements traditional REST endpoints for all entities:
// - Notes: /v1/notes
// - Tasks: /v1/tasks
// - Comments: /v1/comments
// - Chats: /v1/chats
// - Chat Messages: /v1/chat_messages
//
// All mutations go through the same service layer as delta sync, ensuring
// LWW semantics are maintained. REST writes automatically appear in subsequent
// /v1/sync/<entity>/pull operations.
//
// Endpoints per entity:
// - GET    /<entity>              - List (cursor pagination, supports ?includeDeleted=true)
// - POST   /<entity>              - Create (server generates UID if missing)
// - GET    /<entity>/{uid}        - Retrieve single
// - PUT    /<entity>/{uid}        - Replace (full update, supports If-Match)
// - PATCH  /<entity>/{uid}        - Partial update
// - DELETE /<entity>/{uid}        - Soft delete
// - POST   /<entity>/{uid}/archive - Archive (sets status/archived field)
// - POST   /<entity>/{uid}/process - Process action (state machine transitions)
//
// ============================================================================

// parseUIDParam extracts and validates UID from URL parameter
func parseUIDParam(r *http.Request) (uuid.UUID, bool) {
	uidStr := chi.URLParam(r, "uid")
	if uidStr == "" {
		return uuid.Nil, false
	}
	uid, err := uuid.Parse(uidStr)
	if err != nil {
		return uuid.Nil, false
	}
	return uid, true
}

// parseIfMatchHeader extracts version from If-Match header
// Handles both quoted ETags (If-Match: "5") and unquoted (If-Match: 5)
// per RFC 7232 section 2.3
func parseIfMatchHeader(r *http.Request) (int, bool) {
	ifMatch := r.Header.Get("If-Match")
	if ifMatch == "" {
		return 0, false
	}

	// Strip surrounding quotes if present (ETags are typically quoted per RFC 7232)
	// This handles both `If-Match: "5"` and `If-Match: 5`
	etag := ifMatch
	if len(etag) >= 2 && etag[0] == '"' && etag[len(etag)-1] == '"' {
		etag = etag[1 : len(etag)-1]
	}

	version, err := strconv.Atoi(etag)
	if err != nil {
		return 0, false
	}
	return version, true
}

// parseIncludeDeleted parses ?includeDeleted query param
func parseIncludeDeleted(r *http.Request) bool {
	return r.URL.Query().Get("includeDeleted") == "true"
}

// ============================================================================
// Notes Handlers
// ============================================================================

// ListNotes handles GET /v1/notes
func (s *Server) ListNotes(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	// Parse pagination params
	limit := parseLimit(r.URL.Query().Get("limit"), 500, 1000)
	cur, ok := syncx.DecodeCursor(r.URL.Query().Get("cursor"))
	if !ok {
		cur = syncx.Cursor{Ms: 0, UID: uuid.Nil}
	}
	includeDeleted := parseIncludeDeleted(r)

	// Call service
	resp, err := s.NoteSvc.ListNotes(ctx, userID, cur, limit, includeDeleted)
	if err != nil {
		logger.Error().Err(err).Msg("failed to list notes")
		writeError(w, r, 500, "failed to list notes")
		return
	}

	writeJSON(w, 200, resp)
}

// CreateNote handles POST /v1/notes
func (s *Server) CreateNote(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Create note (server generates UID if missing)
	item, err := s.NoteSvc.ApplyNoteMutation(ctx, userID, payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to create note")
		writeError(w, r, 500, "failed to create note")
		return
	}

	writeJSON(w, 201, item)
}

// GetNote handles GET /v1/notes/{uid}
func (s *Server) GetNote(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	includeDeleted := parseIncludeDeleted(r)
	item, err := s.NoteSvc.GetNote(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get note")
		writeError(w, r, 500, "failed to get note")
		return
	}

	if item == nil {
		writeError(w, r, 404, "note not found")
		return
	}

	// Check if deleted
	if item.DeletedAt != nil && !includeDeleted {
		writeJSON(w, 410, map[string]any{
			"error":     "note deleted",
			"deletedAt": item.DeletedAt,
		})
		return
	}

	writeJSON(w, 200, item)
}

// UpdateNote handles PUT /v1/notes/{uid}
func (s *Server) UpdateNote(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Check if note exists and is not deleted
	existing, err := s.NoteSvc.GetNote(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get note for update")
		writeError(w, r, 500, "failed to get note")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "note not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "note deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Ensure UID in payload matches URL
	payload["uid"] = uid.String()

	// Check for optimistic locking
	opts := syncservice.MutationOpts{}
	usedIfMatch := false
	if version, ok := parseIfMatchHeader(r); ok {
		opts.EnforceVersion = true
		opts.ExpectedVersion = version
		usedIfMatch = true
	}

	item, err := s.NoteSvc.ApplyNoteMutation(ctx, userID, payload, opts)
	if err != nil {
		// Check for version mismatch
		if _, ok := err.(*syncservice.VersionMismatchError); ok {
			// RFC 7232: Return 412 Precondition Failed for If-Match failures
			statusCode := 412
			if !usedIfMatch {
				statusCode = 409 // Conflict for other version mismatches
			}
			writeError(w, r, statusCode, "version mismatch: "+err.Error())
			return
		}
		logger.Error().Err(err).Msg("failed to update note")
		writeError(w, r, 500, "failed to update note")
		return
	}

	writeJSON(w, 200, item)
}

// PatchNote handles PATCH /v1/notes/{uid}
func (s *Server) PatchNote(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Fetch existing note
	existing, err := s.NoteSvc.GetNote(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get note for patch")
		writeError(w, r, 500, "failed to get note")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "note not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "note deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Parse partial update
	var partial map[string]any
	if err := json.NewDecoder(r.Body).Decode(&partial); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Merge partial into existing payload
	merged := existing.Payload
	for k, v := range partial {
		if k != "uid" && k != "sync" { // Don't allow overriding sync metadata
			merged[k] = v
		}
	}

	// Apply mutation
	opts := syncservice.MutationOpts{}
	usedIfMatch := false
	if version, ok := parseIfMatchHeader(r); ok {
		opts.EnforceVersion = true
		opts.ExpectedVersion = version
		usedIfMatch = true
	}

	item, err := s.NoteSvc.ApplyNoteMutation(ctx, userID, merged, opts)
	if err != nil {
		if _, ok := err.(*syncservice.VersionMismatchError); ok {
			// RFC 7232: Return 412 Precondition Failed for If-Match failures
			statusCode := 412
			if !usedIfMatch {
				statusCode = 409 // Conflict for other version mismatches
			}
			writeError(w, r, statusCode, "version mismatch: "+err.Error())
			return
		}
		logger.Error().Err(err).Msg("failed to patch note")
		writeError(w, r, 500, "failed to patch note")
		return
	}

	writeJSON(w, 200, item)
}

// DeleteNote handles DELETE /v1/notes/{uid}
func (s *Server) DeleteNote(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Fetch existing note to get payload
	existing, err := s.NoteSvc.GetNote(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get note for delete")
		writeError(w, r, 500, "failed to get note")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "note not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "note already deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Soft delete
	opts := syncservice.MutationOpts{SetDeleted: true}
	item, err := s.NoteSvc.ApplyNoteMutation(ctx, userID, existing.Payload, opts)
	if err != nil {
		logger.Error().Err(err).Msg("failed to delete note")
		writeError(w, r, 500, "failed to delete note")
		return
	}

	writeJSON(w, 200, map[string]any{
		"uid":       item.UID,
		"version":   item.Version,
		"updatedAt": item.UpdatedAt,
		"deletedAt": item.DeletedAt,
	})
}

// ArchiveNote handles POST /v1/notes/{uid}/archive
func (s *Server) ArchiveNote(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Fetch existing note
	existing, err := s.NoteSvc.GetNote(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get note for archive")
		writeError(w, r, 500, "failed to get note")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "note not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "note deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Set archived status
	payload := existing.Payload
	payload["status"] = "archived"

	item, err := s.NoteSvc.ApplyNoteMutation(ctx, userID, payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to archive note")
		writeError(w, r, 500, "failed to archive note")
		return
	}

	writeJSON(w, 200, item)
}

// ProcessNote handles POST /v1/notes/{uid}/process
func (s *Server) ProcessNote(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Parse action
	var req struct {
		Action   string         `json:"action"`
		Metadata map[string]any `json:"metadata,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Fetch existing note
	existing, err := s.NoteSvc.GetNote(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get note for process")
		writeError(w, r, 500, "failed to get note")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "note not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "note deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Apply action
	payload := existing.Payload
	switch req.Action {
	case "pin":
		payload["pinned"] = true
	case "unpin":
		payload["pinned"] = false
	case "archive":
		payload["status"] = "archived"
	case "unarchive":
		payload["status"] = "active"
	default:
		writeError(w, r, 400, "invalid action: "+req.Action)
		return
	}

	item, err := s.NoteSvc.ApplyNoteMutation(ctx, userID, payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to process note")
		writeError(w, r, 500, "failed to process note")
		return
	}

	writeJSON(w, 200, item)
}

// ============================================================================
// Tasks Handlers
// ============================================================================

// ListTasks handles GET /v1/tasks
func (s *Server) ListTasks(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	// Parse pagination params
	limit := parseLimit(r.URL.Query().Get("limit"), 500, 1000)
	cur, ok := syncx.DecodeCursor(r.URL.Query().Get("cursor"))
	if !ok {
		cur = syncx.Cursor{Ms: 0, UID: uuid.Nil}
	}
	includeDeleted := parseIncludeDeleted(r)

	// Call service
	resp, err := s.TaskSvc.ListTasks(ctx, userID, cur, limit, includeDeleted)
	if err != nil {
		logger.Error().Err(err).Msg("failed to list tasks")
		writeError(w, r, 500, "failed to list tasks")
		return
	}

	writeJSON(w, 200, resp)
}

// CreateTask handles POST /v1/tasks
func (s *Server) CreateTask(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Create task (server generates UID if missing)
	item, err := s.TaskSvc.ApplyTaskMutation(ctx, userID, payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to create task")
		writeError(w, r, 500, "failed to create task")
		return
	}

	writeJSON(w, 201, item)
}

// GetTask handles GET /v1/tasks/{uid}
func (s *Server) GetTask(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	includeDeleted := parseIncludeDeleted(r)
	item, err := s.TaskSvc.GetTask(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get task")
		writeError(w, r, 500, "failed to get task")
		return
	}

	if item == nil {
		writeError(w, r, 404, "task not found")
		return
	}

	// Check if deleted
	if item.DeletedAt != nil && !includeDeleted {
		writeJSON(w, 410, map[string]any{
			"error":     "task deleted",
			"deletedAt": item.DeletedAt,
		})
		return
	}

	writeJSON(w, 200, item)
}

// UpdateTask handles PUT /v1/tasks/{uid}
func (s *Server) UpdateTask(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Check if task exists and is not deleted
	existing, err := s.TaskSvc.GetTask(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get task for update")
		writeError(w, r, 500, "failed to get task")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "task not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "task deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Ensure UID in payload matches URL
	payload["uid"] = uid.String()

	// Check for optimistic locking
	opts := syncservice.MutationOpts{}
	usedIfMatch := false
	if version, ok := parseIfMatchHeader(r); ok {
		opts.EnforceVersion = true
		opts.ExpectedVersion = version
		usedIfMatch = true
	}

	item, err := s.TaskSvc.ApplyTaskMutation(ctx, userID, payload, opts)
	if err != nil {
		// Check for version mismatch
		if _, ok := err.(*syncservice.VersionMismatchError); ok {
			// RFC 7232: Return 412 Precondition Failed for If-Match failures
			statusCode := 412
			if !usedIfMatch {
				statusCode = 409 // Conflict for other version mismatches
			}
			writeError(w, r, statusCode, "version mismatch: "+err.Error())
			return
		}
		logger.Error().Err(err).Msg("failed to update task")
		writeError(w, r, 500, "failed to update task")
		return
	}

	writeJSON(w, 200, item)
}

// PatchTask handles PATCH /v1/tasks/{uid}
func (s *Server) PatchTask(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Fetch existing task
	existing, err := s.TaskSvc.GetTask(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get task for patch")
		writeError(w, r, 500, "failed to get task")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "task not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "task deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Parse partial update
	var partial map[string]any
	if err := json.NewDecoder(r.Body).Decode(&partial); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Merge partial into existing payload
	merged := existing.Payload
	for k, v := range partial {
		if k != "uid" && k != "sync" { // Don't allow overriding sync metadata
			merged[k] = v
		}
	}

	// Apply mutation
	opts := syncservice.MutationOpts{}
	usedIfMatch := false
	if version, ok := parseIfMatchHeader(r); ok {
		opts.EnforceVersion = true
		opts.ExpectedVersion = version
		usedIfMatch = true
	}

	item, err := s.TaskSvc.ApplyTaskMutation(ctx, userID, merged, opts)
	if err != nil {
		if _, ok := err.(*syncservice.VersionMismatchError); ok {
			// RFC 7232: Return 412 Precondition Failed for If-Match failures
			statusCode := 412
			if !usedIfMatch {
				statusCode = 409 // Conflict for other version mismatches
			}
			writeError(w, r, statusCode, "version mismatch: "+err.Error())
			return
		}
		logger.Error().Err(err).Msg("failed to patch task")
		writeError(w, r, 500, "failed to patch task")
		return
	}

	writeJSON(w, 200, item)
}

// DeleteTask handles DELETE /v1/tasks/{uid}
func (s *Server) DeleteTask(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Fetch existing task to get payload
	existing, err := s.TaskSvc.GetTask(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get task for delete")
		writeError(w, r, 500, "failed to get task")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "task not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "task already deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Soft delete
	opts := syncservice.MutationOpts{SetDeleted: true}
	item, err := s.TaskSvc.ApplyTaskMutation(ctx, userID, existing.Payload, opts)
	if err != nil {
		logger.Error().Err(err).Msg("failed to delete task")
		writeError(w, r, 500, "failed to delete task")
		return
	}

	writeJSON(w, 200, map[string]any{
		"uid":       item.UID,
		"version":   item.Version,
		"updatedAt": item.UpdatedAt,
		"deletedAt": item.DeletedAt,
	})
}

// ArchiveTask handles POST /v1/tasks/{uid}/archive
func (s *Server) ArchiveTask(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Fetch existing task
	existing, err := s.TaskSvc.GetTask(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get task for archive")
		writeError(w, r, 500, "failed to get task")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "task not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "task deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Set archived status
	payload := existing.Payload
	payload["status"] = "archived"

	item, err := s.TaskSvc.ApplyTaskMutation(ctx, userID, payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to archive task")
		writeError(w, r, 500, "failed to archive task")
		return
	}

	writeJSON(w, 200, item)
}

// ProcessTask handles POST /v1/tasks/{uid}/process
func (s *Server) ProcessTask(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Parse action
	var req struct {
		Action   string         `json:"action"`
		Metadata map[string]any `json:"metadata,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Fetch existing task
	existing, err := s.TaskSvc.GetTask(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get task for process")
		writeError(w, r, 500, "failed to get task")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "task not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "task deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Apply action
	payload := existing.Payload
	switch req.Action {
	case "start":
		payload["status"] = "in_progress"
	case "complete":
		payload["status"] = "completed"
	case "reopen":
		payload["status"] = "open"
	default:
		writeError(w, r, 400, "invalid action: "+req.Action)
		return
	}

	item, err := s.TaskSvc.ApplyTaskMutation(ctx, userID, payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to process task")
		writeError(w, r, 500, "failed to process task")
		return
	}

	writeJSON(w, 200, item)
}

// ============================================================================
// Chats Handlers
// ============================================================================

// ListChats handles GET /v1/chats
func (s *Server) ListChats(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	// Parse pagination params
	limit := parseLimit(r.URL.Query().Get("limit"), 500, 1000)
	cur, ok := syncx.DecodeCursor(r.URL.Query().Get("cursor"))
	if !ok {
		cur = syncx.Cursor{Ms: 0, UID: uuid.Nil}
	}
	includeDeleted := parseIncludeDeleted(r)

	// Call service
	resp, err := s.ChatSvc.ListChats(ctx, userID, cur, limit, includeDeleted)
	if err != nil {
		logger.Error().Err(err).Msg("failed to list chats")
		writeError(w, r, 500, "failed to list chats")
		return
	}

	writeJSON(w, 200, resp)
}

// CreateChat handles POST /v1/chats
func (s *Server) CreateChat(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Create chat (server generates UID if missing)
	item, err := s.ChatSvc.ApplyChatMutation(ctx, userID, payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to create chat")
		writeError(w, r, 500, "failed to create chat")
		return
	}

	writeJSON(w, 201, item)
}

// GetChat handles GET /v1/chats/{uid}
func (s *Server) GetChat(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	includeDeleted := parseIncludeDeleted(r)
	item, err := s.ChatSvc.GetChat(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get chat")
		writeError(w, r, 500, "failed to get chat")
		return
	}

	if item == nil {
		writeError(w, r, 404, "chat not found")
		return
	}

	// Check if deleted
	if item.DeletedAt != nil && !includeDeleted {
		writeJSON(w, 410, map[string]any{
			"error":     "chat deleted",
			"deletedAt": item.DeletedAt,
		})
		return
	}

	writeJSON(w, 200, item)
}

// UpdateChat handles PUT /v1/chats/{uid}
func (s *Server) UpdateChat(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Check if chat exists and is not deleted
	existing, err := s.ChatSvc.GetChat(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get chat for update")
		writeError(w, r, 500, "failed to get chat")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "chat not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "chat deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Ensure UID in payload matches URL
	payload["uid"] = uid.String()

	// Check for optimistic locking
	opts := syncservice.MutationOpts{}
	usedIfMatch := false
	if version, ok := parseIfMatchHeader(r); ok {
		opts.EnforceVersion = true
		opts.ExpectedVersion = version
		usedIfMatch = true
	}

	item, err := s.ChatSvc.ApplyChatMutation(ctx, userID, payload, opts)
	if err != nil {
		// Check for version mismatch
		if _, ok := err.(*syncservice.VersionMismatchError); ok {
			// RFC 7232: Return 412 Precondition Failed for If-Match failures
			statusCode := 412
			if !usedIfMatch {
				statusCode = 409 // Conflict for other version mismatches
			}
			writeError(w, r, statusCode, "version mismatch: "+err.Error())
			return
		}
		logger.Error().Err(err).Msg("failed to update chat")
		writeError(w, r, 500, "failed to update chat")
		return
	}

	writeJSON(w, 200, item)
}

// PatchChat handles PATCH /v1/chats/{uid}
func (s *Server) PatchChat(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Fetch existing chat
	existing, err := s.ChatSvc.GetChat(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get chat for patch")
		writeError(w, r, 500, "failed to get chat")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "chat not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "chat deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Parse partial update
	var partial map[string]any
	if err := json.NewDecoder(r.Body).Decode(&partial); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Merge partial into existing payload
	merged := existing.Payload
	for k, v := range partial {
		if k != "uid" && k != "sync" { // Don't allow overriding sync metadata
			merged[k] = v
		}
	}

	// Apply mutation
	opts := syncservice.MutationOpts{}
	usedIfMatch := false
	if version, ok := parseIfMatchHeader(r); ok {
		opts.EnforceVersion = true
		opts.ExpectedVersion = version
		usedIfMatch = true
	}

	item, err := s.ChatSvc.ApplyChatMutation(ctx, userID, merged, opts)
	if err != nil {
		if _, ok := err.(*syncservice.VersionMismatchError); ok {
			// RFC 7232: Return 412 Precondition Failed for If-Match failures
			statusCode := 412
			if !usedIfMatch {
				statusCode = 409 // Conflict for other version mismatches
			}
			writeError(w, r, statusCode, "version mismatch: "+err.Error())
			return
		}
		logger.Error().Err(err).Msg("failed to patch chat")
		writeError(w, r, 500, "failed to patch chat")
		return
	}

	writeJSON(w, 200, item)
}

// DeleteChat handles DELETE /v1/chats/{uid}
func (s *Server) DeleteChat(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Fetch existing chat to get payload
	existing, err := s.ChatSvc.GetChat(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get chat for delete")
		writeError(w, r, 500, "failed to get chat")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "chat not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "chat already deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Soft delete
	opts := syncservice.MutationOpts{SetDeleted: true}
	item, err := s.ChatSvc.ApplyChatMutation(ctx, userID, existing.Payload, opts)
	if err != nil {
		logger.Error().Err(err).Msg("failed to delete chat")
		writeError(w, r, 500, "failed to delete chat")
		return
	}

	writeJSON(w, 200, map[string]any{
		"uid":       item.UID,
		"version":   item.Version,
		"updatedAt": item.UpdatedAt,
		"deletedAt": item.DeletedAt,
	})
}

// ArchiveChat handles POST /v1/chats/{uid}/archive
func (s *Server) ArchiveChat(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Fetch existing chat
	existing, err := s.ChatSvc.GetChat(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get chat for archive")
		writeError(w, r, 500, "failed to get chat")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "chat not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "chat deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Set archived field
	payload := existing.Payload
	payload["archived"] = true

	item, err := s.ChatSvc.ApplyChatMutation(ctx, userID, payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to archive chat")
		writeError(w, r, 500, "failed to archive chat")
		return
	}

	writeJSON(w, 200, item)
}

// ProcessChat handles POST /v1/chats/{uid}/process
func (s *Server) ProcessChat(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Parse action
	var req struct {
		Action   string         `json:"action"`
		Metadata map[string]any `json:"metadata,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Fetch existing chat
	existing, err := s.ChatSvc.GetChat(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get chat for process")
		writeError(w, r, 500, "failed to get chat")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "chat not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "chat deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Apply action
	payload := existing.Payload
	switch req.Action {
	case "resolve":
		payload["status"] = "resolved"
	case "reopen":
		payload["status"] = "active"
	default:
		writeError(w, r, 400, "invalid action: "+req.Action)
		return
	}

	item, err := s.ChatSvc.ApplyChatMutation(ctx, userID, payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to process chat")
		writeError(w, r, 500, "failed to process chat")
		return
	}

	writeJSON(w, 200, item)
}

// ============================================================================
// Comments Handlers
// ============================================================================

// ListComments handles GET /v1/comments
func (s *Server) ListComments(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	// Parse pagination params
	limit := parseLimit(r.URL.Query().Get("limit"), 500, 1000)
	cur, ok := syncx.DecodeCursor(r.URL.Query().Get("cursor"))
	if !ok {
		cur = syncx.Cursor{Ms: 0, UID: uuid.Nil}
	}
	includeDeleted := parseIncludeDeleted(r)

	// Call service
	resp, err := s.CommentSvc.ListComments(ctx, userID, cur, limit, includeDeleted)
	if err != nil {
		logger.Error().Err(err).Msg("failed to list comments")
		writeError(w, r, 500, "failed to list comments")
		return
	}

	writeJSON(w, 200, resp)
}

// CreateComment handles POST /v1/comments
func (s *Server) CreateComment(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Create comment (server generates UID if missing)
	item, err := s.CommentSvc.ApplyCommentMutation(ctx, userID, payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to create comment")
		writeError(w, r, 500, "failed to create comment")
		return
	}

	writeJSON(w, 201, item)
}

// GetComment handles GET /v1/comments/{uid}
func (s *Server) GetComment(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	includeDeleted := parseIncludeDeleted(r)
	item, err := s.CommentSvc.GetComment(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get comment")
		writeError(w, r, 500, "failed to get comment")
		return
	}

	if item == nil {
		writeError(w, r, 404, "comment not found")
		return
	}

	// Check if deleted
	if item.DeletedAt != nil && !includeDeleted {
		writeJSON(w, 410, map[string]any{
			"error":     "comment deleted",
			"deletedAt": item.DeletedAt,
		})
		return
	}

	writeJSON(w, 200, item)
}

// UpdateComment handles PUT /v1/comments/{uid}
func (s *Server) UpdateComment(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Check if comment exists and is not deleted
	existing, err := s.CommentSvc.GetComment(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get comment for update")
		writeError(w, r, 500, "failed to get comment")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "comment not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "comment deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Ensure UID in payload matches URL
	payload["uid"] = uid.String()

	// Check for optimistic locking
	opts := syncservice.MutationOpts{}
	usedIfMatch := false
	if version, ok := parseIfMatchHeader(r); ok {
		opts.EnforceVersion = true
		opts.ExpectedVersion = version
		usedIfMatch = true
	}

	item, err := s.CommentSvc.ApplyCommentMutation(ctx, userID, payload, opts)
	if err != nil {
		// Check for version mismatch
		if _, ok := err.(*syncservice.VersionMismatchError); ok {
			// RFC 7232: Return 412 Precondition Failed for If-Match failures
			statusCode := 412
			if !usedIfMatch {
				statusCode = 409 // Conflict for other version mismatches
			}
			writeError(w, r, statusCode, "version mismatch: "+err.Error())
			return
		}
		logger.Error().Err(err).Msg("failed to update comment")
		writeError(w, r, 500, "failed to update comment")
		return
	}

	writeJSON(w, 200, item)
}

// PatchComment handles PATCH /v1/comments/{uid}
func (s *Server) PatchComment(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Fetch existing comment
	existing, err := s.CommentSvc.GetComment(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get comment for patch")
		writeError(w, r, 500, "failed to get comment")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "comment not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "comment deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Parse partial update
	var partial map[string]any
	if err := json.NewDecoder(r.Body).Decode(&partial); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Merge partial into existing payload
	merged := existing.Payload
	for k, v := range partial {
		if k != "uid" && k != "sync" { // Don't allow overriding sync metadata
			merged[k] = v
		}
	}

	// Apply mutation
	opts := syncservice.MutationOpts{}
	usedIfMatch := false
	if version, ok := parseIfMatchHeader(r); ok {
		opts.EnforceVersion = true
		opts.ExpectedVersion = version
		usedIfMatch = true
	}

	item, err := s.CommentSvc.ApplyCommentMutation(ctx, userID, merged, opts)
	if err != nil {
		if _, ok := err.(*syncservice.VersionMismatchError); ok {
			// RFC 7232: Return 412 Precondition Failed for If-Match failures
			statusCode := 412
			if !usedIfMatch {
				statusCode = 409 // Conflict for other version mismatches
			}
			writeError(w, r, statusCode, "version mismatch: "+err.Error())
			return
		}
		logger.Error().Err(err).Msg("failed to patch comment")
		writeError(w, r, 500, "failed to patch comment")
		return
	}

	writeJSON(w, 200, item)
}

// DeleteComment handles DELETE /v1/comments/{uid}
func (s *Server) DeleteComment(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Fetch existing comment to get payload
	existing, err := s.CommentSvc.GetComment(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get comment for delete")
		writeError(w, r, 500, "failed to get comment")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "comment not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "comment already deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Soft delete
	opts := syncservice.MutationOpts{SetDeleted: true}
	item, err := s.CommentSvc.ApplyCommentMutation(ctx, userID, existing.Payload, opts)
	if err != nil {
		logger.Error().Err(err).Msg("failed to delete comment")
		writeError(w, r, 500, "failed to delete comment")
		return
	}

	writeJSON(w, 200, map[string]any{
		"uid":       item.UID,
		"version":   item.Version,
		"updatedAt": item.UpdatedAt,
		"deletedAt": item.DeletedAt,
	})
}

// ArchiveComment handles POST /v1/comments/{uid}/archive
func (s *Server) ArchiveComment(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Fetch existing comment
	existing, err := s.CommentSvc.GetComment(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get comment for archive")
		writeError(w, r, 500, "failed to get comment")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "comment not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "comment deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Set archived status
	payload := existing.Payload
	payload["status"] = "archived"

	item, err := s.CommentSvc.ApplyCommentMutation(ctx, userID, payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to archive comment")
		writeError(w, r, 500, "failed to archive comment")
		return
	}

	writeJSON(w, 200, item)
}

// ProcessComment handles POST /v1/comments/{uid}/process
func (s *Server) ProcessComment(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Parse action
	var req struct {
		Action   string         `json:"action"`
		Metadata map[string]any `json:"metadata,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Fetch existing comment
	existing, err := s.CommentSvc.GetComment(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get comment for process")
		writeError(w, r, 500, "failed to get comment")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "comment not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "comment deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Apply action
	payload := existing.Payload
	switch req.Action {
	case "resolve":
		payload["status"] = "resolved"
	case "reopen":
		payload["status"] = "open"
	default:
		writeError(w, r, 400, "invalid action: "+req.Action)
		return
	}

	item, err := s.CommentSvc.ApplyCommentMutation(ctx, userID, payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to process comment")
		writeError(w, r, 500, "failed to process comment")
		return
	}

	writeJSON(w, 200, item)
}

// ============================================================================
// Chat Messages Handlers
// ============================================================================

// ListChatMessages handles GET /v1/chat_messages
func (s *Server) ListChatMessages(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	// Parse pagination params
	limit := parseLimit(r.URL.Query().Get("limit"), 500, 1000)
	cur, ok := syncx.DecodeCursor(r.URL.Query().Get("cursor"))
	if !ok {
		cur = syncx.Cursor{Ms: 0, UID: uuid.Nil}
	}
	includeDeleted := parseIncludeDeleted(r)

	// Call service
	resp, err := s.ChatMessageSvc.ListChatMessages(ctx, userID, cur, limit, includeDeleted)
	if err != nil {
		logger.Error().Err(err).Msg("failed to list chat messages")
		writeError(w, r, 500, "failed to list chat messages")
		return
	}

	writeJSON(w, 200, resp)
}

// CreateChatMessage handles POST /v1/chat_messages
func (s *Server) CreateChatMessage(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Create chat message (server generates UID if missing)
	item, err := s.ChatMessageSvc.ApplyChatMessageMutation(ctx, userID, payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to create chat message")
		writeError(w, r, 500, "failed to create chat message")
		return
	}

	writeJSON(w, 201, item)
}

// GetChatMessage handles GET /v1/chat_messages/{uid}
func (s *Server) GetChatMessage(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	includeDeleted := parseIncludeDeleted(r)
	item, err := s.ChatMessageSvc.GetChatMessage(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get chat message")
		writeError(w, r, 500, "failed to get chat message")
		return
	}

	if item == nil {
		writeError(w, r, 404, "chat message not found")
		return
	}

	// Check if deleted
	if item.DeletedAt != nil && !includeDeleted {
		writeJSON(w, 410, map[string]any{
			"error":     "chat message deleted",
			"deletedAt": item.DeletedAt,
		})
		return
	}

	writeJSON(w, 200, item)
}

// UpdateChatMessage handles PUT /v1/chat_messages/{uid}
func (s *Server) UpdateChatMessage(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Check if chat message exists and is not deleted
	existing, err := s.ChatMessageSvc.GetChatMessage(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get chat message for update")
		writeError(w, r, 500, "failed to get chat message")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "chat message not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "chat message deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Ensure UID in payload matches URL
	payload["uid"] = uid.String()

	// Check for optimistic locking
	opts := syncservice.MutationOpts{}
	usedIfMatch := false
	if version, ok := parseIfMatchHeader(r); ok {
		opts.EnforceVersion = true
		opts.ExpectedVersion = version
		usedIfMatch = true
	}

	item, err := s.ChatMessageSvc.ApplyChatMessageMutation(ctx, userID, payload, opts)
	if err != nil {
		// Check for version mismatch
		if _, ok := err.(*syncservice.VersionMismatchError); ok {
			// RFC 7232: Return 412 Precondition Failed for If-Match failures
			statusCode := 412
			if !usedIfMatch {
				statusCode = 409 // Conflict for other version mismatches
			}
			writeError(w, r, statusCode, "version mismatch: "+err.Error())
			return
		}
		logger.Error().Err(err).Msg("failed to update chat message")
		writeError(w, r, 500, "failed to update chat message")
		return
	}

	writeJSON(w, 200, item)
}

// PatchChatMessage handles PATCH /v1/chat_messages/{uid}
func (s *Server) PatchChatMessage(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Fetch existing chat message
	existing, err := s.ChatMessageSvc.GetChatMessage(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get chat message for patch")
		writeError(w, r, 500, "failed to get chat message")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "chat message not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "chat message deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Parse partial update
	var partial map[string]any
	if err := json.NewDecoder(r.Body).Decode(&partial); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Merge partial into existing payload
	merged := existing.Payload
	for k, v := range partial {
		if k != "uid" && k != "sync" { // Don't allow overriding sync metadata
			merged[k] = v
		}
	}

	// Apply mutation
	opts := syncservice.MutationOpts{}
	usedIfMatch := false
	if version, ok := parseIfMatchHeader(r); ok {
		opts.EnforceVersion = true
		opts.ExpectedVersion = version
		usedIfMatch = true
	}

	item, err := s.ChatMessageSvc.ApplyChatMessageMutation(ctx, userID, merged, opts)
	if err != nil {
		if _, ok := err.(*syncservice.VersionMismatchError); ok {
			// RFC 7232: Return 412 Precondition Failed for If-Match failures
			statusCode := 412
			if !usedIfMatch {
				statusCode = 409 // Conflict for other version mismatches
			}
			writeError(w, r, statusCode, "version mismatch: "+err.Error())
			return
		}
		logger.Error().Err(err).Msg("failed to patch chat message")
		writeError(w, r, 500, "failed to patch chat message")
		return
	}

	writeJSON(w, 200, item)
}

// DeleteChatMessage handles DELETE /v1/chat_messages/{uid}
func (s *Server) DeleteChatMessage(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Fetch existing chat message to get payload
	existing, err := s.ChatMessageSvc.GetChatMessage(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get chat message for delete")
		writeError(w, r, 500, "failed to get chat message")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "chat message not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "chat message already deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Soft delete
	opts := syncservice.MutationOpts{SetDeleted: true}
	item, err := s.ChatMessageSvc.ApplyChatMessageMutation(ctx, userID, existing.Payload, opts)
	if err != nil {
		logger.Error().Err(err).Msg("failed to delete chat message")
		writeError(w, r, 500, "failed to delete chat message")
		return
	}

	writeJSON(w, 200, map[string]any{
		"uid":       item.UID,
		"version":   item.Version,
		"updatedAt": item.UpdatedAt,
		"deletedAt": item.DeletedAt,
	})
}

// ArchiveChatMessage handles POST /v1/chat_messages/{uid}/archive
func (s *Server) ArchiveChatMessage(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Fetch existing chat message
	existing, err := s.ChatMessageSvc.GetChatMessage(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get chat message for archive")
		writeError(w, r, 500, "failed to get chat message")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "chat message not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "chat message deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Set archived status
	payload := existing.Payload
	payload["archived"] = true

	item, err := s.ChatMessageSvc.ApplyChatMessageMutation(ctx, userID, payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to archive chat message")
		writeError(w, r, 500, "failed to archive chat message")
		return
	}

	writeJSON(w, 200, item)
}

// ProcessChatMessage handles POST /v1/chat_messages/{uid}/process
func (s *Server) ProcessChatMessage(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()
	logger := log.Ctx(ctx)

	uid, ok := parseUIDParam(r)
	if !ok {
		writeError(w, r, 400, "invalid UID")
		return
	}

	// Parse action
	var req struct {
		Action   string         `json:"action"`
		Metadata map[string]any `json:"metadata,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, 400, "invalid JSON")
		return
	}

	// Fetch existing chat message
	existing, err := s.ChatMessageSvc.GetChatMessage(ctx, userID, uid)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get chat message for process")
		writeError(w, r, 500, "failed to get chat message")
		return
	}
	if existing == nil {
		writeError(w, r, 404, "chat message not found")
		return
	}
	if existing.DeletedAt != nil {
		writeJSON(w, 410, map[string]any{
			"error":     "chat message deleted",
			"deletedAt": existing.DeletedAt,
		})
		return
	}

	// Apply action
	payload := existing.Payload
	switch req.Action {
	case "mark_read":
		payload["read"] = true
	case "mark_delivered":
		payload["delivered"] = true
	default:
		writeError(w, r, 400, "invalid action: "+req.Action)
		return
	}

	item, err := s.ChatMessageSvc.ApplyChatMessageMutation(ctx, userID, payload, syncservice.MutationOpts{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to process chat message")
		writeError(w, r, 500, "failed to process chat message")
		return
	}

	writeJSON(w, 200, item)
}
