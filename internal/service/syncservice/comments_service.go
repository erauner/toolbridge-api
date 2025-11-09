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
