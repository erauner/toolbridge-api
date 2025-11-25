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

// PushAck represents the server response for a single pushed item
type PushAck struct {
	UID       string `json:"uid"`
	Version   int    `json:"version"`
	UpdatedAt string `json:"updatedAt"`
	Error     string `json:"error,omitempty"`
	Applied   bool   `json:"applied,omitempty"`
}

// PullResponse represents the response from a pull operation
type PullResponse struct {
	Upserts    []map[string]any `json:"upserts"`
	Deletes    []map[string]any `json:"deletes"`
	NextCursor *string          `json:"nextCursor,omitempty"`
}

// NoteService encapsulates business logic for note sync operations
type NoteService struct {
	DB *pgxpool.Pool
}

// NewNoteService creates a new NoteService
func NewNoteService(db *pgxpool.Pool) *NoteService {
	return &NoteService{DB: db}
}

// PushNoteItem handles the push logic for a single note item within a transaction
// Returns a PushAck with either success or error information
func (s *NoteService) PushNoteItem(ctx context.Context, tx pgx.Tx, userID string, item map[string]any) PushAck {
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
	// If same timestamp arrives twice, version doesn't increment
	tag, err := tx.Exec(ctx, `
		INSERT INTO note (uid, owner_id, updated_at_ms, deleted_at_ms, version, payload_json)
		VALUES ($1, $2, $3, $4, GREATEST($5, 1), $6)
		ON CONFLICT (owner_id, uid) DO UPDATE SET
			payload_json   = EXCLUDED.payload_json,
			updated_at_ms  = EXCLUDED.updated_at_ms,
			deleted_at_ms  = EXCLUDED.deleted_at_ms,
			-- Bump version only on strictly newer update (not >=, just >)
			version        = CASE
				WHEN EXCLUDED.updated_at_ms > note.updated_at_ms
				THEN note.version + 1
				ELSE note.version
			END
		WHERE EXCLUDED.updated_at_ms > note.updated_at_ms
	`, ext.UID, userID, ext.UpdatedAtMs, ext.DeletedAtMs, ext.Version, payloadJSON)

	applied := false
	if err == nil {
		applied = tag.RowsAffected() > 0
	}

	if err != nil {
		logger.Error().Err(err).Str("uid", ext.UID.String()).Msg("failed to upsert note")
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
		`SELECT version, updated_at_ms FROM note WHERE uid = $1 AND owner_id = $2`,
		ext.UID, userID).Scan(&serverVersion, &serverMs); err != nil {
		logger.Error().Err(err).Str("uid", ext.UID.String()).Msg("failed to read note after upsert")
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
		Applied:   applied,
	}
}

// PullNotes handles the pull logic for notes
// Returns upserts, deletes, and an optional next cursor for pagination
func (s *NoteService) PullNotes(ctx context.Context, userID string, cursor syncx.Cursor, limit int) (*PullResponse, error) {
	logger := log.With().Logger()

	// Query notes ordered by (updated_at_ms, uid) for deterministic pagination
	rows, err := s.DB.Query(ctx, `
		SELECT payload_json, deleted_at_ms, updated_at_ms, uid
		FROM note
		WHERE owner_id = $1
		  AND (updated_at_ms, uid) > ($2, $3::uuid)
		ORDER BY updated_at_ms, uid
		LIMIT $4
	`, userID, cursor.Ms, cursor.UID, limit)

	if err != nil {
		logger.Error().Err(err).Msg("failed to query notes")
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
			logger.Error().Err(err).Msg("failed to scan note row")
			return nil, err
		}

		if deletedAtMs != nil {
			// Tombstone - return as delete
			deletes = append(deletes, map[string]any{
				"uid":       uid,
				"deletedAt": syncx.RFC3339(*deletedAtMs),
			})
		} else {
			// Active note - return full payload
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

// GetNote retrieves a single note by UID
// Returns the item regardless of deletion status (handler decides 404 vs 410)
func (s *NoteService) GetNote(ctx context.Context, userID string, uid uuid.UUID) (*RESTItem, error) {
	logger := log.With().Logger()

	var payload map[string]any
	var version int
	var updatedAtMs int64
	var deletedAtMs *int64

	err := s.DB.QueryRow(ctx, `
		SELECT payload_json, version, updated_at_ms, deleted_at_ms
		FROM note
		WHERE owner_id = $1 AND uid = $2
	`, userID, uid).Scan(&payload, &version, &updatedAtMs, &deletedAtMs)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil // Not found
		}
		logger.Error().Err(err).Str("uid", uid.String()).Msg("failed to get note")
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

// ListNotes returns paginated notes for REST endpoints
func (s *NoteService) ListNotes(ctx context.Context, userID string, cursor syncx.Cursor, limit int, includeDeleted bool) (*RESTListResponse, error) {
	logger := log.With().Logger()

	// Build query based on includeDeleted
	query := `
		SELECT payload_json, deleted_at_ms, updated_at_ms, uid, version
		FROM note
		WHERE owner_id = $1
		  AND (updated_at_ms, uid) > ($2, $3::uuid)
	`
	if !includeDeleted {
		query += ` AND deleted_at_ms IS NULL`
	}
	query += ` ORDER BY updated_at_ms, uid LIMIT $4`

	rows, err := s.DB.Query(ctx, query, userID, cursor.Ms, cursor.UID, limit)
	if err != nil {
		logger.Error().Err(err).Msg("failed to list notes")
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
			logger.Error().Err(err).Msg("failed to scan note row")
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

// ApplyNoteMutation creates or updates a note via REST
// Handles optimistic locking, monotonic timestamps, and soft deletes
func (s *NoteService) ApplyNoteMutation(ctx context.Context, userID string, payload map[string]any, opts MutationOpts) (*RESTItem, error) {
	logger := log.With().Logger()

	// Start transaction
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("failed to begin transaction")
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Extract UID or generate new one
	var noteUID uuid.UUID
	if uidStr, ok := syncx.GetString(payload, "uid"); ok {
		noteUID, _ = uuid.Parse(uidStr)
	}
	if noteUID == uuid.Nil {
		noteUID = uuid.New()
		payload["uid"] = noteUID.String()
	}

	// Fetch existing note to determine timestamp
	var existingMs int64
	var existingVersion int
	err = tx.QueryRow(ctx, `
		SELECT updated_at_ms, version
		FROM note
		WHERE owner_id = $1 AND uid = $2
	`, userID, noteUID).Scan(&existingMs, &existingVersion)

	if err != nil && err != pgx.ErrNoRows {
		logger.Error().Err(err).Msg("failed to probe existing note")
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
	ack := s.PushNoteItem(ctx, tx, userID, mutatedPayload)
	if ack.Error != "" {
		return nil, &MutationError{Message: ack.Error}
	}

	// Detect whether our mutation actually advanced the row.
	// Use the Applied flag from PushNoteItem (rows affected) to avoid clobbering when
	// the LWW guard rejected an equal-timestamp or concurrent update.
	upsertApplied := ack.Applied
	var deletedAtFromDB *string

	// Normalize sync metadata in payload for client compatibility
	// Flutter clients expect flat sync fields (version, isDirty, isDeleted, remoteUpdatedAt, updateTime)
	// to match the nested sync block that BuildServerMutation created.

	// Only write the normalized payload when our upsert actually won (timestamps matched)
	// Otherwise we risk overwriting a newer concurrent write with stale content.
	if upsertApplied {
		// Update nested sync.version to match authoritative server version
		if syncBlock, ok := mutatedPayload["sync"].(map[string]any); ok {
			syncBlock["version"] = ack.Version
		}

		// Normalize flat sync fields to match nested sync and server state
		mutatedPayload["version"] = ack.Version
		mutatedPayload["isDirty"] = false // REST mutations are already synced
		if opts.SetDeleted {
			mutatedPayload["isDeleted"] = true
		} else {
			mutatedPayload["isDeleted"] = false
		}
		mutatedPayload["remoteUpdatedAt"] = ack.UpdatedAt
		mutatedPayload["updateTime"] = ack.UpdatedAt
		mutatedPayload["lastSyncedAt"] = ack.UpdatedAt

		// Persist normalized payload to database
		payloadJSON, err := json.Marshal(mutatedPayload)
		if err != nil {
			logger.Error().Err(err).Msg("failed to marshal normalized payload")
			return nil, err
		}

		if _, err = tx.Exec(ctx, `
			UPDATE note
			SET payload_json = $1::jsonb
			WHERE owner_id = $2 AND uid = $3
		`, payloadJSON, userID, noteUID); err != nil {
			logger.Error().Err(err).Msg("failed to update normalized payload")
			return nil, err
		}
	} else {
		logger.Warn().
			Str("uid", noteUID.String()).
			Int("existingVersion", existingVersion).
			Int("ackVersion", ack.Version).
			Msg("skipping payload normalization because a newer write already exists")
		// Refresh payload for response to reflect the current authoritative state
		var currentPayload map[string]any
		var deletedAtMs *int64
		if err := tx.QueryRow(ctx, `
			SELECT payload_json, deleted_at_ms
			FROM note
			WHERE owner_id = $1 AND uid = $2
		`, userID, noteUID).Scan(&currentPayload, &deletedAtMs); err != nil {
			logger.Error().Err(err).Msg("failed to reload payload after concurrent write")
			return nil, err
		}
		mutatedPayload = currentPayload
		if deletedAtMs != nil {
			ts := syncx.RFC3339(*deletedAtMs)
			deletedAtFromDB = &ts
		}
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Error().Err(err).Msg("failed to commit mutation")
		return nil, err
	}

	// Return item
	var deletedAt *string
	if upsertApplied && opts.SetDeleted {
		ts := syncx.RFC3339(timestampMs)
		deletedAt = &ts
	}

	if !upsertApplied {
		if deletedAtFromDB != nil {
			deletedAt = deletedAtFromDB
		} else if syncBlock, ok := mutatedPayload["sync"].(map[string]any); ok {
			if isDeleted, ok := syncBlock["isDeleted"].(bool); ok && isDeleted {
				// Best-effort: use ack.UpdatedAt when no explicit deletedAt was provided
				ts := ack.UpdatedAt
				deletedAt = &ts
			}
		}
	}

	return &RESTItem{
		UID:       ack.UID,
		Version:   ack.Version,
		UpdatedAt: ack.UpdatedAt,
		DeletedAt: deletedAt,
		Payload:   mutatedPayload,
	}, nil
}
