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

// TaskListCategoryService encapsulates business logic for task list category sync operations
type TaskListCategoryService struct {
	DB *pgxpool.Pool
}

// NewTaskListCategoryService creates a new TaskListCategoryService
func NewTaskListCategoryService(db *pgxpool.Pool) *TaskListCategoryService {
	return &TaskListCategoryService{DB: db}
}

// PushTaskListCategoryItem handles the push logic for a single category item within a transaction
func (s *TaskListCategoryService) PushTaskListCategoryItem(ctx context.Context, tx pgx.Tx, userID string, item map[string]any) PushAck {
	logger := log.With().Logger()

	ext, err := syncx.ExtractCommon(item)
	if err != nil {
		logger.Warn().Err(err).Interface("item", item).Msg("failed to extract sync metadata")
		return PushAck{Error: err.Error()}
	}

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

	_, err = tx.Exec(ctx, `
		INSERT INTO task_list_category (uid, owner_id, updated_at_ms, deleted_at_ms, version, payload_json)
		VALUES ($1, $2, $3, $4, GREATEST($5, 1), $6)
		ON CONFLICT (owner_id, uid) DO UPDATE SET
			payload_json   = EXCLUDED.payload_json,
			updated_at_ms  = EXCLUDED.updated_at_ms,
			deleted_at_ms  = EXCLUDED.deleted_at_ms,
			version        = CASE
				WHEN EXCLUDED.updated_at_ms > task_list_category.updated_at_ms
				THEN task_list_category.version + 1
				ELSE task_list_category.version
			END
		WHERE EXCLUDED.updated_at_ms > task_list_category.updated_at_ms
	`, ext.UID, userID, ext.UpdatedAtMs, ext.DeletedAtMs, ext.Version, payloadJSON)

	if err != nil {
		logger.Error().Err(err).Str("uid", ext.UID.String()).Msg("failed to upsert task_list_category")
		return PushAck{
			UID:       ext.UID.String(),
			Version:   ext.Version,
			UpdatedAt: syncx.RFC3339(ext.UpdatedAtMs),
			Error:     err.Error(),
		}
	}

	var serverVersion int
	var serverMs int64
	if err := tx.QueryRow(ctx,
		`SELECT version, updated_at_ms FROM task_list_category WHERE uid = $1 AND owner_id = $2`,
		ext.UID, userID).Scan(&serverVersion, &serverMs); err != nil {
		logger.Error().Err(err).Str("uid", ext.UID.String()).Msg("failed to read task_list_category after upsert")
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

// PullTaskListCategories handles the pull logic for task list categories
func (s *TaskListCategoryService) PullTaskListCategories(ctx context.Context, userID string, cursor syncx.Cursor, limit int) (*PullResponse, error) {
	logger := log.With().Logger()

	rows, err := s.DB.Query(ctx, `
		SELECT payload_json, deleted_at_ms, updated_at_ms, uid
		FROM task_list_category
		WHERE owner_id = $1
		  AND (updated_at_ms, uid) > ($2, $3::uuid)
		ORDER BY updated_at_ms, uid
		LIMIT $4
	`, userID, cursor.Ms, cursor.UID, limit)

	if err != nil {
		logger.Error().Err(err).Msg("failed to query task_list_categories")
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
			logger.Error().Err(err).Msg("failed to scan task_list_category row")
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

// GetTaskListCategory retrieves a single category by UID
func (s *TaskListCategoryService) GetTaskListCategory(ctx context.Context, userID string, uid uuid.UUID) (*RESTItem, error) {
	logger := log.With().Logger()

	var payload map[string]any
	var version int
	var updatedAtMs int64
	var deletedAtMs *int64

	err := s.DB.QueryRow(ctx, `
		SELECT payload_json, version, updated_at_ms, deleted_at_ms
		FROM task_list_category
		WHERE owner_id = $1 AND uid = $2
	`, userID, uid).Scan(&payload, &version, &updatedAtMs, &deletedAtMs)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		logger.Error().Err(err).Str("uid", uid.String()).Msg("failed to get task_list_category")
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

// ListTaskListCategories returns paginated categories for REST endpoints
func (s *TaskListCategoryService) ListTaskListCategories(ctx context.Context, userID string, cursor syncx.Cursor, limit int, includeDeleted bool) (*RESTListResponse, error) {
	logger := log.With().Logger()

	query := `
		SELECT payload_json, deleted_at_ms, updated_at_ms, uid, version
		FROM task_list_category
		WHERE owner_id = $1
		  AND (updated_at_ms, uid) > ($2, $3::uuid)
	`
	if !includeDeleted {
		query += ` AND deleted_at_ms IS NULL`
	}
	query += ` ORDER BY updated_at_ms, uid LIMIT $4`

	rows, err := s.DB.Query(ctx, query, userID, cursor.Ms, cursor.UID, limit)
	if err != nil {
		logger.Error().Err(err).Msg("failed to list task_list_categories")
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
			logger.Error().Err(err).Msg("failed to scan task_list_category row")
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

// ApplyTaskListCategoryMutation creates or updates a category via REST
func (s *TaskListCategoryService) ApplyTaskListCategoryMutation(ctx context.Context, userID string, payload map[string]any, opts MutationOpts) (*RESTItem, error) {
	logger := log.With().Logger()

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("failed to begin transaction")
		return nil, err
	}
	defer tx.Rollback(ctx)

	var categoryUID uuid.UUID
	if uidStr, ok := syncx.GetString(payload, "uid"); ok {
		categoryUID, _ = uuid.Parse(uidStr)
	}
	if categoryUID == uuid.Nil {
		categoryUID = uuid.New()
		payload["uid"] = categoryUID.String()
	}

	var existingMs int64
	var existingVersion int
	err = tx.QueryRow(ctx, `
		SELECT updated_at_ms, version
		FROM task_list_category
		WHERE owner_id = $1 AND uid = $2
	`, userID, categoryUID).Scan(&existingMs, &existingVersion)

	if err != nil && err != pgx.ErrNoRows {
		logger.Error().Err(err).Msg("failed to probe existing task_list_category")
		return nil, err
	}

	isNew := err == pgx.ErrNoRows

	if !isNew && opts.EnforceVersion {
		if existingVersion != opts.ExpectedVersion {
			return nil, &VersionMismatchError{
				Expected: opts.ExpectedVersion,
				Actual:   existingVersion,
			}
		}
	}

	var timestampMs int64
	if opts.ForceTimestampMs != nil {
		timestampMs = *opts.ForceTimestampMs
	} else if isNew {
		timestampMs = syncx.NowMs()
	} else {
		timestampMs = syncx.EnsureMonotonicTimestamp(existingMs)
	}

	mutatedPayload := syncx.BuildServerMutation(payload, timestampMs, opts.SetDeleted)

	ack := s.PushTaskListCategoryItem(ctx, tx, userID, mutatedPayload)
	if ack.Error != "" {
		return nil, &MutationError{Message: ack.Error}
	}

	_, err = tx.Exec(ctx, `
		UPDATE task_list_category
		SET payload_json = jsonb_set(payload_json, '{sync,version}', to_jsonb($1::int))
		WHERE owner_id = $2 AND uid = $3
	`, ack.Version, userID, categoryUID)
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
