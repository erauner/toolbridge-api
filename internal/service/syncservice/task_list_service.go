package syncservice

import (
	"context"
	"encoding/json"

	"github.com/erauner12/toolbridge-api/internal/syncx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// TaskListService encapsulates business logic for task list sync operations
type TaskListService struct {
	DB *pgxpool.Pool
}

// NewTaskListService creates a new TaskListService
func NewTaskListService(db *pgxpool.Pool) *TaskListService {
	return &TaskListService{DB: db}
}

// PushTaskListItem handles the push logic for a single task list item within a transaction
// Returns a PushAck with either success or error information
func (s *TaskListService) PushTaskListItem(ctx context.Context, tx pgx.Tx, userID string, item map[string]any) PushAck {
	logger := log.With().Logger()

	// Extract sync metadata from client JSON
	ext, err := syncx.ExtractCommon(item)
	if err != nil {
		logger.Warn().Err(err).Interface("item", item).Msg("failed to extract sync metadata")
		return PushAck{Error: err.Error()}
	}

	// Serialize payload back to JSON for storage
	payloadJSON, err := json.Marshal(item)
	if err != nil {
		logger.Error().Err(err).Str("uid", ext.UID.String()).Msg("failed to marshal payload")
		return PushAck{
			UID:       ext.UID.String(),
			Version:   ext.Version,
			UpdatedAt: syncx.RFC3339(ext.UpdatedAtMs),
			Error:     "payload serialization error",
		}
	}

	// Insert or update with LWW conflict resolution
	// Key invariant: WHERE clause uses strict > (not >=) to make duplicate pushes idempotent
	_, err = tx.Exec(ctx, `
		INSERT INTO task_list (uid, owner_id, updated_at_ms, deleted_at_ms, version, payload_json)
		VALUES ($1, $2, $3, $4, GREATEST($5, 1), $6)
		ON CONFLICT (owner_id, uid) DO UPDATE SET
			payload_json   = EXCLUDED.payload_json,
			updated_at_ms  = EXCLUDED.updated_at_ms,
			deleted_at_ms  = EXCLUDED.deleted_at_ms,
			version        = CASE
				WHEN EXCLUDED.updated_at_ms > task_list.updated_at_ms
				THEN task_list.version + 1
				ELSE task_list.version
			END
		WHERE EXCLUDED.updated_at_ms > task_list.updated_at_ms
	`, ext.UID, userID, ext.UpdatedAtMs, ext.DeletedAtMs, ext.Version, payloadJSON)

	if err != nil {
		logger.Error().Err(err).Str("uid", ext.UID.String()).Msg("failed to upsert task_list")
		return PushAck{
			UID:       ext.UID.String(),
			Version:   ext.Version,
			UpdatedAt: syncx.RFC3339(ext.UpdatedAtMs),
			Error:     err.Error(),
		}
	}

	// Read back server state (authoritative version and timestamp)
	var serverVersion int
	var serverMs int64
	if err := tx.QueryRow(ctx,
		`SELECT version, updated_at_ms FROM task_list WHERE uid = $1 AND owner_id = $2`,
		ext.UID, userID).Scan(&serverVersion, &serverMs); err != nil {
		logger.Error().Err(err).Str("uid", ext.UID.String()).Msg("failed to read task_list after upsert")
		return PushAck{
			UID:       ext.UID.String(),
			Version:   ext.Version,
			UpdatedAt: syncx.RFC3339(ext.UpdatedAtMs),
			Error:     "failed to confirm write",
		}
	}

	return PushAck{
		UID:       ext.UID.String(),
		Version:   serverVersion,
		UpdatedAt: syncx.RFC3339(serverMs),
	}
}

// PullTaskLists handles the pull logic for task lists
func (s *TaskListService) PullTaskLists(ctx context.Context, userID string, cursor syncx.Cursor, limit int) (*PullResponse, error) {
	logger := log.With().Logger()

	rows, err := s.DB.Query(ctx, `
		SELECT payload_json, deleted_at_ms, updated_at_ms, uid
		FROM task_list
		WHERE owner_id = $1
		  AND (updated_at_ms, uid) > ($2, $3::uuid)
		ORDER BY updated_at_ms, uid
		LIMIT $4
	`, userID, cursor.Ms, cursor.UID, limit)

	if err != nil {
		logger.Error().Err(err).Msg("failed to query task_lists")
		return nil, err
	}
	defer rows.Close()

	upserts := make([]map[string]any, 0, limit)
	deletes := make([]map[string]any, 0)
	var lastMs int64
	var lastUID string

	for rows.Next() {
		var payload map[string]any
		var deletedAtMs *int64
		var ms int64
		var uid string

		if err := rows.Scan(&payload, &deletedAtMs, &ms, &uid); err != nil {
			logger.Error().Err(err).Msg("failed to scan task_list row")
			return nil, err
		}

		if deletedAtMs != nil {
			deletes = append(deletes, map[string]any{
				"uid":       uid,
				"deletedAt": syncx.RFC3339(*deletedAtMs),
			})
		} else {
			upserts = append(upserts, payload)
		}

		lastMs, lastUID = ms, uid
	}

	if err := rows.Err(); err != nil {
		logger.Error().Err(err).Msg("row iteration error")
		return nil, err
	}

	var nextCursor *string
	if len(upserts)+len(deletes) > 0 {
		uid, _ := uuid.Parse(lastUID)
		encoded := syncx.EncodeCursor(syncx.Cursor{Ms: lastMs, UID: uid})
		nextCursor = &encoded
	}

	return &PullResponse{
		Upserts:    upserts,
		Deletes:    deletes,
		NextCursor: nextCursor,
	}, nil
}

// GetTaskList retrieves a single task list by UID
func (s *TaskListService) GetTaskList(ctx context.Context, userID string, uid uuid.UUID) (*RESTItem, error) {
	logger := log.With().Logger()

	var payload map[string]any
	var version int
	var updatedAtMs int64
	var deletedAtMs *int64

	err := s.DB.QueryRow(ctx, `
		SELECT payload_json, version, updated_at_ms, deleted_at_ms
		FROM task_list
		WHERE owner_id = $1 AND uid = $2
	`, userID, uid).Scan(&payload, &version, &updatedAtMs, &deletedAtMs)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		logger.Error().Err(err).Str("uid", uid.String()).Msg("failed to get task_list")
		return nil, err
	}

	item := &RESTItem{
		UID:       uid.String(),
		Version:   version,
		UpdatedAt: syncx.RFC3339(updatedAtMs),
		Payload:   payload,
	}

	if deletedAtMs != nil {
		deletedAt := syncx.RFC3339(*deletedAtMs)
		item.DeletedAt = &deletedAt
	}

	return item, nil
}

// ListTaskLists returns paginated task lists for REST endpoints
func (s *TaskListService) ListTaskLists(ctx context.Context, userID string, cursor syncx.Cursor, limit int, includeDeleted bool) (*RESTListResponse, error) {
	logger := log.With().Logger()

	query := `
		SELECT payload_json, deleted_at_ms, updated_at_ms, uid, version
		FROM task_list
		WHERE owner_id = $1
		  AND (updated_at_ms, uid) > ($2, $3::uuid)
	`
	if !includeDeleted {
		query += ` AND deleted_at_ms IS NULL`
	}
	query += ` ORDER BY updated_at_ms, uid LIMIT $4`

	rows, err := s.DB.Query(ctx, query, userID, cursor.Ms, cursor.UID, limit)
	if err != nil {
		logger.Error().Err(err).Msg("failed to list task_lists")
		return nil, err
	}
	defer rows.Close()

	items := make([]RESTItem, 0, limit)
	var lastMs int64
	var lastUID string

	for rows.Next() {
		var payload map[string]any
		var deletedAtMs *int64
		var ms int64
		var uid string
		var version int

		if err := rows.Scan(&payload, &deletedAtMs, &ms, &uid, &version); err != nil {
			logger.Error().Err(err).Msg("failed to scan task_list row")
			return nil, err
		}

		item := RESTItem{
			UID:       uid,
			Version:   version,
			UpdatedAt: syncx.RFC3339(ms),
			Payload:   payload,
		}

		if deletedAtMs != nil {
			deletedAt := syncx.RFC3339(*deletedAtMs)
			item.DeletedAt = &deletedAt
		}

		items = append(items, item)
		lastMs, lastUID = ms, uid
	}

	if err := rows.Err(); err != nil {
		logger.Error().Err(err).Msg("row iteration error")
		return nil, err
	}

	var nextCursor *string
	if len(items) > 0 {
		uid, _ := uuid.Parse(lastUID)
		encoded := syncx.EncodeCursor(syncx.Cursor{Ms: lastMs, UID: uid})
		nextCursor = &encoded
	}

	return &RESTListResponse{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// ApplyTaskListMutation creates or updates a task list via REST
func (s *TaskListService) ApplyTaskListMutation(ctx context.Context, userID string, payload map[string]any, opts MutationOpts) (*RESTItem, error) {
	logger := log.With().Logger()

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("failed to begin transaction")
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Extract UID or generate new one
	var taskListUID uuid.UUID
	if uidStr, ok := syncx.GetString(payload, "uid"); ok {
		taskListUID, _ = uuid.Parse(uidStr)
	}
	if taskListUID == uuid.Nil {
		taskListUID = uuid.New()
		payload["uid"] = taskListUID.String()
	}

	// Fetch existing to determine timestamp
	var existingMs int64
	var existingVersion int
	err = tx.QueryRow(ctx, `
		SELECT updated_at_ms, version
		FROM task_list
		WHERE owner_id = $1 AND uid = $2
	`, userID, taskListUID).Scan(&existingMs, &existingVersion)

	if err != nil && err != pgx.ErrNoRows {
		logger.Error().Err(err).Msg("failed to probe existing task_list")
		return nil, err
	}

	isNew := err == pgx.ErrNoRows

	// Optimistic locking check
	if !isNew && opts.EnforceVersion {
		if existingVersion != opts.ExpectedVersion {
			return nil, &VersionMismatchError{
				Expected: opts.ExpectedVersion,
				Actual:   existingVersion,
			}
		}
	}

	// Determine timestamp (monotonic)
	var timestampMs int64
	if opts.ForceTimestampMs != nil {
		timestampMs = *opts.ForceTimestampMs
	} else if isNew {
		timestampMs = syncx.NowMs()
	} else {
		timestampMs = syncx.EnsureMonotonicTimestamp(existingMs)
	}

	// Build sync-compliant payload
	mutatedPayload := syncx.BuildServerMutation(payload, timestampMs, opts.SetDeleted)

	// Call existing push logic
	ack := s.PushTaskListItem(ctx, tx, userID, mutatedPayload)
	if ack.Error != "" {
		return nil, &MutationError{Message: ack.Error}
	}

	// Fix payload's sync.version to match the authoritative server version
	_, err = tx.Exec(ctx, `
		UPDATE task_list
		SET payload_json = jsonb_set(payload_json, '{sync,version}', to_jsonb($1::int))
		WHERE owner_id = $2 AND uid = $3
	`, ack.Version, userID, taskListUID)
	if err != nil {
		logger.Error().Err(err).Msg("failed to update payload version")
		return nil, err
	}

	if syncBlock, ok := mutatedPayload["sync"].(map[string]any); ok {
		syncBlock["version"] = ack.Version
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error().Err(err).Msg("failed to commit mutation")
		return nil, err
	}

	var deletedAt *string
	if opts.SetDeleted {
		ts := syncx.RFC3339(timestampMs)
		deletedAt = &ts
	}

	return &RESTItem{
		UID:       ack.UID,
		Version:   ack.Version,
		UpdatedAt: ack.UpdatedAt,
		DeletedAt: deletedAt,
		Payload:   mutatedPayload,
	}, nil
}

// OrphanTasksInList sets taskListUid to null for all tasks in a given list
// This is called when deleting a task list to preserve tasks as standalone
func (s *TaskListService) OrphanTasksInList(ctx context.Context, userID string, taskListUID uuid.UUID) (int64, error) {
	logger := log.With().Logger()

	// Update tasks that belong to this list by removing taskListUid from payload
	// Also bump updated_at_ms to ensure changes sync
	result, err := s.DB.Exec(ctx, `
		UPDATE task
		SET payload_json = payload_json - 'taskListUid',
		    updated_at_ms = $3,
		    version = version + 1
		WHERE owner_id = $1
		  AND payload_json->>'taskListUid' = $2
		  AND deleted_at_ms IS NULL
	`, userID, taskListUID.String(), syncx.NowMs())

	if err != nil {
		logger.Error().Err(err).Str("taskListUid", taskListUID.String()).Msg("failed to orphan tasks")
		return 0, err
	}

	return result.RowsAffected(), nil
}
