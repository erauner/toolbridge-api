package syncservice

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/erauner12/toolbridge-api/internal/syncx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// CommentService encapsulates business logic for comment sync operations
type CommentService struct {
	DB *pgxpool.Pool
}

// NewCommentService creates a new CommentService
func NewCommentService(db *pgxpool.Pool) *CommentService {
	return &CommentService{DB: db}
}

// PushCommentItem handles the push logic for a single comment item within a transaction
// Returns a PushAck with either success or error information
// Validates that parent (note or task) exists before upserting
func (s *CommentService) PushCommentItem(ctx context.Context, tx pgx.Tx, userID string, item map[string]any) PushAck {
	logger := log.With().Logger()

	// Extract sync metadata + parent fields from client JSON
	ext, err := syncx.ExtractComment(item)
	if err != nil {
		logger.Warn().Err(err).Interface("item", item).Msg("failed to extract sync metadata")
		return PushAck{Error: err.Error()}
	}

	// Validate parent type
	if ext.ParentType != "note" && ext.ParentType != "task" {
		logger.Warn().Str("parent_type", ext.ParentType).Msg("invalid parent type")
		return PushAck{
			UID:       ext.UID.String(),
			Version:   ext.Version,
			UpdatedAt: syncx.RFC3339(ext.UpdatedAtMs),
			Error:     fmt.Sprintf("invalid parent_type: %s (must be 'note' or 'task')", ext.ParentType),
		}
	}

	// Only validate parent exists if we're NOT deleting the comment
	// If deleting, we don't care about parent state (it may already be deleted)
	// This allows comment tombstones to succeed even after parent is deleted
	if ext.DeletedAtMs == nil {
		// Validate parent exists AND is not soft-deleted (critical for referential integrity)
		var parentExists bool
		if ext.ParentType == "note" {
			err := tx.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM note WHERE owner_id = $1 AND uid = $2 AND deleted_at_ms IS NULL)`,
				userID, *ext.ParentUID).Scan(&parentExists)
			if err != nil {
				logger.Error().Err(err).Str("parent_uid", ext.ParentUID.String()).Msg("failed to check note existence")
				return PushAck{
					UID:       ext.UID.String(),
					Version:   ext.Version,
					UpdatedAt: syncx.RFC3339(ext.UpdatedAtMs),
					Error:     "failed to validate parent",
				}
			}
		} else if ext.ParentType == "task" {
			err := tx.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM task WHERE owner_id = $1 AND uid = $2 AND deleted_at_ms IS NULL)`,
				userID, *ext.ParentUID).Scan(&parentExists)
			if err != nil {
				logger.Error().Err(err).Str("parent_uid", ext.ParentUID.String()).Msg("failed to check task existence")
				return PushAck{
					UID:       ext.UID.String(),
					Version:   ext.Version,
					UpdatedAt: syncx.RFC3339(ext.UpdatedAtMs),
					Error:     "failed to validate parent",
				}
			}
		}

		if !parentExists {
			logger.Warn().
				Str("parent_type", ext.ParentType).
				Str("parent_uid", ext.ParentUID.String()).
				Msg("parent not found")
			return PushAck{
				UID:       ext.UID.String(),
				Version:   ext.Version,
				UpdatedAt: syncx.RFC3339(ext.UpdatedAtMs),
				Error:     fmt.Sprintf("parent %s not found: %s", ext.ParentType, ext.ParentUID.String()),
			}
		}
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
	// If same timestamp arrives twice, version doesn't increment
	_, err = tx.Exec(ctx, `
		INSERT INTO comment (uid, owner_id, updated_at_ms, deleted_at_ms, version, payload_json, parent_type, parent_uid)
		VALUES ($1, $2, $3, $4, GREATEST($5, 1), $6, $7, $8)
		ON CONFLICT (owner_id, uid) DO UPDATE SET
			payload_json   = EXCLUDED.payload_json,
			updated_at_ms  = EXCLUDED.updated_at_ms,
			deleted_at_ms  = EXCLUDED.deleted_at_ms,
			parent_type    = EXCLUDED.parent_type,
			parent_uid     = EXCLUDED.parent_uid,
			-- Bump version only on strictly newer update (not >=, just >)
			version        = CASE
				WHEN EXCLUDED.updated_at_ms > comment.updated_at_ms
				THEN comment.version + 1
				ELSE comment.version
			END
		WHERE EXCLUDED.updated_at_ms > comment.updated_at_ms
	`, ext.UID, userID, ext.UpdatedAtMs, ext.DeletedAtMs, ext.Version, payloadJSON, ext.ParentType, *ext.ParentUID)

	if err != nil {
		logger.Error().Err(err).Str("uid", ext.UID.String()).Msg("failed to upsert comment")
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
		`SELECT version, updated_at_ms FROM comment WHERE uid = $1 AND owner_id = $2`,
		ext.UID, userID).Scan(&serverVersion, &serverMs); err != nil {
		logger.Error().Err(err).Str("uid", ext.UID.String()).Msg("failed to read comment after upsert")
		return PushAck{
			UID:       ext.UID.String(),
			Version:   ext.Version,
			UpdatedAt: syncx.RFC3339(ext.UpdatedAtMs),
			Error:     "failed to confirm write",
		}
	}

	// Success - return server-authoritative values
	return PushAck{
		UID:       ext.UID.String(),
		Version:   serverVersion,
		UpdatedAt: syncx.RFC3339(serverMs),
	}
}

// PullComments handles the pull logic for comments
// Returns upserts, deletes, and an optional next cursor for pagination
func (s *CommentService) PullComments(ctx context.Context, userID string, cursor syncx.Cursor, limit int) (*PullResponse, error) {
	logger := log.With().Logger()

	// Query comments ordered by (updated_at_ms, uid) for deterministic pagination
	rows, err := s.DB.Query(ctx, `
		SELECT payload_json, deleted_at_ms, updated_at_ms, uid
		FROM comment
		WHERE owner_id = $1
		  AND (updated_at_ms, uid) > ($2, $3::uuid)
		ORDER BY updated_at_ms, uid
		LIMIT $4
	`, userID, cursor.Ms, cursor.UID, limit)

	if err != nil {
		logger.Error().Err(err).Msg("failed to query comments")
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
			logger.Error().Err(err).Msg("failed to scan comment row")
			return nil, err
		}

		if deletedAtMs != nil {
			// Tombstone - return as delete
			deletes = append(deletes, map[string]any{
				"uid":       uid,
				"deletedAt": syncx.RFC3339(*deletedAtMs),
			})
		} else {
			// Active comment - return full payload
			upserts = append(upserts, payload)
		}

		lastMs, lastUID = ms, uid
	}

	if err := rows.Err(); err != nil {
		logger.Error().Err(err).Msg("row iteration error")
		return nil, err
	}

	// Generate next cursor if we returned any results
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

// REST-specific methods

// GetComment retrieves a single comment by UID
// Returns the item regardless of deletion status (handler decides 404 vs 410)
func (s *CommentService) GetComment(ctx context.Context, userID string, uid uuid.UUID) (*RESTItem, error) {
	logger := log.With().Logger()

	var payload map[string]any
	var version int
	var updatedAtMs int64
	var deletedAtMs *int64

	err := s.DB.QueryRow(ctx, `
		SELECT payload_json, version, updated_at_ms, deleted_at_ms
		FROM comment
		WHERE owner_id = $1 AND uid = $2
	`, userID, uid).Scan(&payload, &version, &updatedAtMs, &deletedAtMs)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil // Not found
		}
		logger.Error().Err(err).Str("uid", uid.String()).Msg("failed to get comment")
		return nil, err
	}

	// Always return the item (even if deleted) - handler will decide 410 vs 200
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

// ListComments returns paginated comments for REST endpoints
func (s *CommentService) ListComments(ctx context.Context, userID string, cursor syncx.Cursor, limit int, includeDeleted bool) (*RESTListResponse, error) {
	logger := log.With().Logger()

	// Build query based on includeDeleted
	query := `
		SELECT payload_json, deleted_at_ms, updated_at_ms, uid, version
		FROM comment
		WHERE owner_id = $1
		  AND (updated_at_ms, uid) > ($2, $3::uuid)
	`
	if !includeDeleted {
		query += ` AND deleted_at_ms IS NULL`
	}
	query += ` ORDER BY updated_at_ms, uid LIMIT $4`

	rows, err := s.DB.Query(ctx, query, userID, cursor.Ms, cursor.UID, limit)
	if err != nil {
		logger.Error().Err(err).Msg("failed to list comments")
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
			logger.Error().Err(err).Msg("failed to scan comment row")
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

	// Generate next cursor if we have results
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

// ApplyCommentMutation creates or updates a comment via REST
// Handles optimistic locking, monotonic timestamps, and soft deletes
func (s *CommentService) ApplyCommentMutation(ctx context.Context, userID string, payload map[string]any, opts MutationOpts) (*RESTItem, error) {
	logger := log.With().Logger()

	// Start transaction
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("failed to begin transaction")
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Extract UID or generate new one
	var commentUID uuid.UUID
	if uidStr, ok := syncx.GetString(payload, "uid"); ok {
		commentUID, _ = uuid.Parse(uidStr)
	}
	if commentUID == uuid.Nil {
		commentUID = uuid.New()
		payload["uid"] = commentUID.String()
	}

	// Fetch existing comment to determine timestamp
	var existingMs int64
	var existingVersion int
	err = tx.QueryRow(ctx, `
		SELECT updated_at_ms, version
		FROM comment
		WHERE owner_id = $1 AND uid = $2
	`, userID, commentUID).Scan(&existingMs, &existingVersion)

	if err != nil && err != pgx.ErrNoRows {
		logger.Error().Err(err).Msg("failed to probe existing comment")
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
	ack := s.PushCommentItem(ctx, tx, userID, mutatedPayload)
	if ack.Error != "" {
		return nil, &MutationError{Message: ack.Error}
	}

	// Fix payload's sync.version to match the authoritative server version
	// This ensures delta-sync clients see the correct version in the payload
	_, err = tx.Exec(ctx, `
		UPDATE comment
		SET payload_json = jsonb_set(payload_json, '{sync,version}', to_jsonb($1::int))
		WHERE owner_id = $2 AND uid = $3
	`, ack.Version, userID, commentUID)
	if err != nil {
		logger.Error().Err(err).Msg("failed to update payload version")
		return nil, err
	}

	// Also update the in-memory payload for the response
	if syncBlock, ok := mutatedPayload["sync"].(map[string]any); ok {
		syncBlock["version"] = ack.Version
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error().Err(err).Msg("failed to commit mutation")
		return nil, err
	}

	// Return item
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
