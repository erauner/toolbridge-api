package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/rs/zerolog/log"
)

type wipeRequest struct {
	Confirm string `json:"confirm"` // Must be "WIPE"
	Mode    string `json:"mode"`    // "hard" (only mode supported currently)
}

type wipeResponse struct {
	Epoch   int            `json:"epoch"`
	Deleted map[string]int `json:"deleted"`
}

// WipeAccount permanently deletes all synced data for the authenticated user.
//
// This operation:
// 1. Bumps the tenant epoch (invalidates all devices)
// 2. Deletes all entity rows owned by the user
// 3. Invalidates all active sessions for the user
//
// Requires:
// - Valid authentication
// - Active sync session (X-Sync-Session header)
// - Confirmation string "WIPE" in request body
//
// Returns:
// - 200: Wipe successful, returns new epoch and deletion counts
// - 400: Missing confirmation or invalid request
// - 401: Unauthorized
// - 500: Database error
func (s *Server) WipeAccount(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse request
	var req wipeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	// Require explicit confirmation
	if req.Confirm != "WIPE" {
		writeError(w, r, http.StatusBadRequest, "confirmation required: must send {\"confirm\":\"WIPE\"}")
		return
	}

	ctx := r.Context()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		log.Error().Err(err).Str("userId", userID).Msg("Failed to begin transaction")
		writeError(w, r, http.StatusInternalServerError, "transaction begin failed")
		return
	}
	defer tx.Rollback(ctx)

	// Bump epoch (atomically)
	var newEpoch int
	err = tx.QueryRow(ctx, `
		INSERT INTO owner_state(owner_id, epoch, last_wipe_at, last_wipe_by, created_at, updated_at)
		VALUES ($1, 2, NOW(), $1, NOW(), NOW())
		ON CONFLICT (owner_id) DO UPDATE
			SET epoch = owner_state.epoch + 1,
				last_wipe_at = NOW(),
				last_wipe_by = EXCLUDED.last_wipe_by,
				updated_at = NOW()
		RETURNING epoch
	`, userID).Scan(&newEpoch)

	if err != nil {
		log.Error().Err(err).Str("userId", userID).Msg("Failed to bump epoch")
		writeError(w, r, http.StatusInternalServerError, "epoch update failed")
		return
	}

	// Delete all entity rows for this user
	// Order matters: delete children before parents (e.g., chat_message before chat)
	deleted := make(map[string]int)
	tables := []string{"chat_message", "comment", "chat", "task", "task_list", "task_list_category", "note"}

	for _, table := range tables {
		var count int
		err := tx.QueryRow(ctx, `
			WITH del AS (
				DELETE FROM `+table+` WHERE owner_id = $1 RETURNING 1
			)
			SELECT COUNT(*) FROM del
		`, userID).Scan(&count)

		if err != nil {
			log.Error().Err(err).Str("table", table).Str("userId", userID).Msg("Failed to delete rows")
			writeError(w, r, http.StatusInternalServerError, "delete failed: "+table)
			return
		}
		deleted[table] = count
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		log.Error().Err(err).Str("userId", userID).Msg("Failed to commit wipe transaction")
		writeError(w, r, http.StatusInternalServerError, "commit failed")
		return
	}

	// Invalidate all sessions for this user (outside transaction)
	sessionsDeleted := sessionStore.DeleteUserSessions(userID)

	log.Info().
		Str("userId", userID).
		Int("newEpoch", newEpoch).
		Interface("deleted", deleted).
		Int("sessionsInvalidated", sessionsDeleted).
		Msg("Account wiped successfully")

	// Return success response
	writeJSON(w, http.StatusOK, wipeResponse{
		Epoch:   newEpoch,
		Deleted: deleted,
	})
}
