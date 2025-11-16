package httpapi

import (
	"net/http"
	"strconv"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// EpochRequired middleware validates that the client's X-Sync-Epoch header
// matches the server's current epoch for the authenticated user.
//
// If the client's epoch is less than the server's epoch, returns 409 Conflict
// with the current epoch in the response body and X-Sync-Epoch header.
//
// This prevents stale clients from pushing/pulling data after a server wipe.
func EpochRequired(db *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := auth.UserID(r.Context())
			if userID == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			// Load current user epoch (lazy create if not exists)
			var epoch int
			err := db.QueryRow(r.Context(), `
				INSERT INTO owner_state(owner_id, epoch, created_at, updated_at)
				VALUES ($1, 1, NOW(), NOW())
				ON CONFLICT (owner_id) DO NOTHING
				RETURNING epoch
			`, userID).Scan(&epoch)

			if err != nil {
				// If insert did nothing, select existing epoch
				if err == pgx.ErrNoRows {
					err = db.QueryRow(r.Context(),
						`SELECT epoch FROM owner_state WHERE owner_id = $1`,
						userID,
					).Scan(&epoch)
					if err != nil {
						log.Error().Err(err).Str("userId", userID).Msg("Failed to load epoch")
						http.Error(w, "epoch load failed", http.StatusInternalServerError)
						return
					}
				} else {
					log.Error().Err(err).Str("userId", userID).Msg("Failed to initialize epoch")
					http.Error(w, "epoch init failed", http.StatusInternalServerError)
					return
				}
			}

			// Compare with client's epoch from header
			clientEpochStr := r.Header.Get("X-Sync-Epoch")
			clientEpoch := 0
			if clientEpochStr != "" {
				clientEpoch, _ = strconv.Atoi(clientEpochStr)
			}

			// If client epoch is behind server epoch, reject with 409
			if clientEpoch < epoch {
				log.Warn().
					Str("userId", userID).
					Int("clientEpoch", clientEpoch).
					Int("serverEpoch", epoch).
					Msg("Epoch mismatch detected - client must reset")

				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Sync-Epoch", strconv.Itoa(epoch))
				w.Header().Set("X-Correlation-ID", r.Header.Get("X-Correlation-ID"))
		
				writeJSON(w, http.StatusConflict, map[string]any{
					"error":          "epoch_mismatch",
					"epoch":          epoch,
					"correlation_id": r.Header.Get("X-Correlation-ID"),
				})
				return
			}

			// Epoch matches or client is ahead (shouldn't happen, but allow)
			next.ServeHTTP(w, r)
		})
	}
}
