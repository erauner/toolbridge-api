package httpapi

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/rs/zerolog/log"
)

type syncStateResponse struct {
	Epoch      int        `json:"epoch"`
	LastWipeAt *time.Time `json:"lastWipeAt,omitempty"`
	LastWipeBy *string    `json:"lastWipeBy,omitempty"`
}

// GetSyncState returns the current sync state for the authenticated user.
//
// Returns:
// - epoch: Current tenant epoch
// - lastWipeAt: Timestamp of last wipe operation (if any)
// - lastWipeBy: User ID who triggered the last wipe (if any)
//
// This endpoint is used by clients to check if a reset is required
// without triggering a full sync operation.
func (s *Server) GetSyncState(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserID(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var epoch int
	var lastWipeAt sql.NullTime
	var lastWipeBy sql.NullString

	err := s.DB.QueryRow(r.Context(), `
		SELECT epoch, last_wipe_at, last_wipe_by
		FROM owner_state
		WHERE owner_id = $1
	`, userID).Scan(&epoch, &lastWipeAt, &lastWipeBy)

	if err != nil {
		// If row doesn't exist, return default state (epoch=1)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusOK, syncStateResponse{
				Epoch: 1,
			})
			return
		}

		log.Error().Err(err).Str("userId", userID).Msg("Failed to load sync state")
		writeError(w, r, http.StatusInternalServerError, "failed to load sync state")
		return
	}

	resp := syncStateResponse{
		Epoch: epoch,
	}

	if lastWipeAt.Valid {
		resp.LastWipeAt = &lastWipeAt.Time
	}
	if lastWipeBy.Valid {
		resp.LastWipeBy = &lastWipeBy.String
	}

	writeJSON(w, http.StatusOK, resp)
}
