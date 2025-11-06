package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/erauner12/toolbridge-api/internal/syncx"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// PushChatMessages handles POST /v1/sync/chat_messages/push
// Implements Last-Write-Wins (LWW) conflict resolution with idempotent pushes
// Validates parent chat exists before upserting
func (s *Server) PushChatMessages(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()

	var req pushReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Warn().Err(err).Msg("invalid push request body")
		writeJSON(w, 400, []pushAck{{Error: "invalid json"}})
		return
	}

	acks := make([]pushAck, 0, len(req.Items))

	// Use transaction for atomicity (all-or-nothing per batch)
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to begin transaction")
		writeJSON(w, 500, []pushAck{{Error: "transaction error"}})
		return
	}
	defer tx.Rollback(ctx)

	for _, item := range req.Items {
		// Extract sync metadata + chat_uid from client JSON
		ext, err := syncx.ExtractChatMessage(item)
		if err != nil {
			log.Warn().Err(err).Interface("item", item).Msg("failed to extract sync metadata")
			acks = append(acks, pushAck{Error: err.Error()})
			continue
		}

		// Only validate parent chat exists if we're NOT deleting the message
		// If deleting, we don't care about parent state (it may already be deleted)
		// This allows message tombstones to succeed even after chat is deleted
		if ext.DeletedAtMs == nil {
			// Validate parent chat exists AND is not soft-deleted (critical for referential integrity)
			var chatExists bool
			err := tx.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM chat WHERE owner_id = $1 AND uid = $2 AND deleted_at_ms IS NULL)`,
				userID, *ext.ChatUID).Scan(&chatExists)
			if err != nil {
				log.Error().Err(err).Str("chat_uid", ext.ChatUID.String()).Msg("failed to check chat existence")
				acks = append(acks, pushAck{
					UID:       ext.UID.String(),
					Version:   ext.Version,
					UpdatedAt: syncx.RFC3339(ext.UpdatedAtMs),
					Error:     "failed to validate parent chat",
				})
				continue
			}

			if !chatExists {
				log.Warn().
					Str("chat_uid", ext.ChatUID.String()).
					Msg("parent chat not found")
				acks = append(acks, pushAck{
					UID:       ext.UID.String(),
					Version:   ext.Version,
					UpdatedAt: syncx.RFC3339(ext.UpdatedAtMs),
					Error:     fmt.Sprintf("parent chat not found: %s", ext.ChatUID.String()),
				})
				continue
			}
		}

		// Serialize payload back to JSON for storage
		payloadJSON, err := json.Marshal(item)
		if err != nil {
			log.Error().Err(err).Str("uid", ext.UID.String()).Msg("failed to marshal payload")
			acks = append(acks, pushAck{
				UID:       ext.UID.String(),
				Version:   ext.Version,
				UpdatedAt: syncx.RFC3339(ext.UpdatedAtMs),
				Error:     "payload serialization error",
			})
			continue
		}

		// Insert or update with LWW conflict resolution
		// Key invariant: WHERE clause uses strict > (not >=) to make duplicate pushes idempotent
		// If same timestamp arrives twice, version doesn't increment
		_, err = tx.Exec(ctx, `
			INSERT INTO chat_message (uid, owner_id, updated_at_ms, deleted_at_ms, version, payload_json, chat_uid)
			VALUES ($1, $2, $3, $4, GREATEST($5, 1), $6, $7)
			ON CONFLICT (owner_id, uid) DO UPDATE SET
				payload_json   = EXCLUDED.payload_json,
				updated_at_ms  = EXCLUDED.updated_at_ms,
				deleted_at_ms  = EXCLUDED.deleted_at_ms,
				chat_uid       = EXCLUDED.chat_uid,
				-- Bump version only on strictly newer update (not >=, just >)
				version        = CASE
					WHEN EXCLUDED.updated_at_ms > chat_message.updated_at_ms
					THEN chat_message.version + 1
					ELSE chat_message.version
				END
			WHERE EXCLUDED.updated_at_ms > chat_message.updated_at_ms
		`, ext.UID, userID, ext.UpdatedAtMs, ext.DeletedAtMs, ext.Version, payloadJSON, *ext.ChatUID)

		if err != nil {
			log.Error().Err(err).Str("uid", ext.UID.String()).Msg("failed to upsert chat_message")
			acks = append(acks, pushAck{
				UID:       ext.UID.String(),
				Version:   ext.Version,
				UpdatedAt: syncx.RFC3339(ext.UpdatedAtMs),
				Error:     err.Error(),
			})
			continue
		}

		// Read back server state (authoritative version and timestamp)
		var serverVersion int
		var serverMs int64
		if err := tx.QueryRow(ctx,
			`SELECT version, updated_at_ms FROM chat_message WHERE uid = $1 AND owner_id = $2`,
			ext.UID, userID).Scan(&serverVersion, &serverMs); err != nil {
			log.Error().Err(err).Str("uid", ext.UID.String()).Msg("failed to read chat_message after upsert")
			acks = append(acks, pushAck{
				UID:       ext.UID.String(),
				Version:   ext.Version,
				UpdatedAt: syncx.RFC3339(ext.UpdatedAtMs),
				Error:     "failed to confirm write",
			})
			continue
		}

		// Success - return server-authoritative values
		acks = append(acks, pushAck{
			UID:       ext.UID.String(),
			Version:   serverVersion,
			UpdatedAt: syncx.RFC3339(serverMs),
		})
	}

	if err := tx.Commit(ctx); err != nil {
		log.Error().Err(err).Msg("failed to commit transaction")
		writeJSON(w, 500, []pushAck{{Error: "commit failed"}})
		return
	}

	writeJSON(w, 200, acks)
}

// PullChatMessages handles GET /v1/sync/chat_messages/pull?cursor=<opaque>&limit=<int>
// Returns upserts and deletes in deterministic order using cursor-based pagination
func (s *Server) PullChatMessages(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	ctx := r.Context()

	// Parse query params
	limit := parseLimit(r.URL.Query().Get("limit"), 500, 1000)
	cur, ok := syncx.DecodeCursor(r.URL.Query().Get("cursor"))
	if !ok {
		// No cursor = start from beginning (epoch)
		cur = syncx.Cursor{Ms: 0, UID: uuid.Nil}
	}

	// Query chat_messages ordered by (updated_at_ms, uid) for deterministic pagination
	rows, err := s.DB.Query(ctx, `
		SELECT payload_json, deleted_at_ms, updated_at_ms, uid
		FROM chat_message
		WHERE owner_id = $1
		  AND (updated_at_ms, uid) > ($2, $3::uuid)
		ORDER BY updated_at_ms, uid
		LIMIT $4
	`, userID, cur.Ms, cur.UID, limit)

	if err != nil {
		log.Error().Err(err).Msg("failed to query chat_messages")
		writeJSON(w, 500, map[string]any{"error": "query failed"})
		return
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
			log.Error().Err(err).Msg("failed to scan chat_message row")
			writeJSON(w, 500, map[string]any{"error": "scan failed"})
			return
		}

		if deletedAtMs != nil {
			// Tombstone - return as delete
			deletes = append(deletes, map[string]any{
				"uid":       uid,
				"deletedAt": syncx.RFC3339(*deletedAtMs),
			})
		} else {
			// Active chat_message - return full payload
			upserts = append(upserts, payload)
		}

		lastMs, lastUID = ms, uid
	}

	if err := rows.Err(); err != nil {
		log.Error().Err(err).Msg("row iteration error")
		writeJSON(w, 500, map[string]any{"error": "iteration failed"})
		return
	}

	// Generate next cursor if we returned any results
	var nextCursor *string
	if len(upserts)+len(deletes) > 0 {
		uid, _ := uuid.Parse(lastUID)
		encoded := syncx.EncodeCursor(syncx.Cursor{Ms: lastMs, UID: uid})
		nextCursor = &encoded
	}

	writeJSON(w, 200, pullResp{
		Upserts:    upserts,
		Deletes:    deletes,
		NextCursor: nextCursor,
	})
}
