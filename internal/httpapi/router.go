package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// Server holds dependencies for HTTP handlers
type Server struct {
	DB *pgxpool.Pool
}

// Common request/response types for sync endpoints

// pushReq is the request body for push endpoints
type pushReq struct {
	Items []map[string]any `json:"items"`
}

// pushAck is a per-item acknowledgment in push responses
type pushAck struct {
	UID       string `json:"uid"`
	Version   int    `json:"version"`
	UpdatedAt string `json:"updatedAt"`
	Error     string `json:"error,omitempty"`
}

// pullResp is the response body for pull endpoints
type pullResp struct {
	Upserts    []map[string]any `json:"upserts"`
	Deletes    []map[string]any `json:"deletes"`
	NextCursor *string          `json:"nextCursor,omitempty"`
}

// writeJSON writes a JSON response with the given status code
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Error().Err(err).Msg("failed to encode json response")
	}
}

// parseLimit parses a limit query param with default and max
func parseLimit(q string, def, max int) int {
	if q == "" {
		return def
	}
	n, err := strconv.Atoi(q)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// Routes creates the HTTP router with all sync endpoints
func (s *Server) Routes(jwt auth.JWTCfg) http.Handler {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Health check (unauthenticated)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})

	// All sync endpoints require authentication
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(s.DB, jwt))

		// Notes
		r.Post("/v1/sync/notes/push", s.PushNotes)
		r.Get("/v1/sync/notes/pull", s.PullNotes)

		// Tasks
		r.Post("/v1/sync/tasks/push", s.PushTasks)
		r.Get("/v1/sync/tasks/pull", s.PullTasks)

		// Comments
		r.Post("/v1/sync/comments/push", s.PushComments)
		r.Get("/v1/sync/comments/pull", s.PullComments)

		// Chats
		r.Post("/v1/sync/chats/push", s.PushChats)
		r.Get("/v1/sync/chats/pull", s.PullChats)

		// Chat Messages
		r.Post("/v1/sync/chat-messages/push", s.PushChatMessages)
		r.Get("/v1/sync/chat-messages/pull", s.PullChatMessages)
	})

	log.Info().Msg("HTTP routes registered")
	return r
}
