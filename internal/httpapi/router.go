package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/erauner12/toolbridge-api/internal/service/syncservice"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// Server holds dependencies for HTTP handlers
type Server struct {
	DB              *pgxpool.Pool
	RateLimitConfig RateLimitInfo // Centralized rate limit configuration
	// Services
	NoteSvc        *syncservice.NoteService
	TaskSvc        *syncservice.TaskService
	CommentSvc     *syncservice.CommentService
	ChatSvc        *syncservice.ChatService
	ChatMessageSvc *syncservice.ChatMessageService
}

// DefaultRateLimitConfig provides the default rate limiting configuration
var DefaultRateLimitConfig = RateLimitInfo{
	WindowSeconds: 60,  // 1 minute window
	MaxRequests:   600, // 600 requests per window (sustained rate)
	Burst:         120, // Allow burst of 120 requests
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

// errorResponse represents a standardized error response with correlation ID
type errorResponse struct {
	Error         string `json:"error"`
	CorrelationID string `json:"correlation_id"`
}

// writeError writes an error response with correlation ID from context
func writeError(w http.ResponseWriter, r *http.Request, code int, message string) {
	correlationID := GetCorrelationID(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(errorResponse{
		Error:         message,
		CorrelationID: correlationID,
	})
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
	r.Use(CorrelationMiddleware) // Track X-Correlation-ID header for request tracing
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(SessionMiddleware) // Track X-Sync-Session header

	// Health check (unauthenticated)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})

	// Server info / capability discovery (unauthenticated)
	r.Get("/v1/sync/info", s.Info)

	// All sync endpoints require authentication
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(s.DB, jwt))

		// Session management (no session or rate limit required for these)
		r.Post("/v1/sync/sessions", s.BeginSession)
		r.Get("/v1/sync/sessions/{id}", s.GetSession)
		r.Delete("/v1/sync/sessions/{id}", s.EndSession)

		// Entity sync endpoints require active session, rate limiting, and epoch validation
		r.Group(func(r chi.Router) {
			r.Use(SessionRequired) // Enforce X-Sync-Session header
			r.Use(RateLimitMiddleware(s.RateLimitConfig))
			r.Use(EpochRequired(s.DB)) // NEW: Validate epoch on all entity operations

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
			r.Post("/v1/sync/chat_messages/push", s.PushChatMessages)
			r.Get("/v1/sync/chat_messages/pull", s.PullChatMessages)
		})

		// REST CRUD endpoints require same protections as sync endpoints
		r.Group(func(r chi.Router) {
			r.Use(SessionRequired)
			r.Use(RateLimitMiddleware(s.RateLimitConfig))
			r.Use(EpochRequired(s.DB))

			// Notes REST endpoints
			r.Get("/v1/notes", s.ListNotes)
			r.Post("/v1/notes", s.CreateNote)
			r.Get("/v1/notes/{uid}", s.GetNote)
			r.Put("/v1/notes/{uid}", s.UpdateNote)
			r.Patch("/v1/notes/{uid}", s.PatchNote)
			r.Delete("/v1/notes/{uid}", s.DeleteNote)
			r.Post("/v1/notes/{uid}/archive", s.ArchiveNote)
			r.Post("/v1/notes/{uid}/process", s.ProcessNote)

			// Tasks REST endpoints
			r.Get("/v1/tasks", s.ListTasks)
			r.Post("/v1/tasks", s.CreateTask)
			r.Get("/v1/tasks/{uid}", s.GetTask)
			r.Put("/v1/tasks/{uid}", s.UpdateTask)
			r.Patch("/v1/tasks/{uid}", s.PatchTask)
			r.Delete("/v1/tasks/{uid}", s.DeleteTask)
			r.Post("/v1/tasks/{uid}/archive", s.ArchiveTask)
			r.Post("/v1/tasks/{uid}/process", s.ProcessTask)

			// Comments REST endpoints
			r.Get("/v1/comments", s.ListComments)
			r.Post("/v1/comments", s.CreateComment)
			r.Get("/v1/comments/{uid}", s.GetComment)
			r.Put("/v1/comments/{uid}", s.UpdateComment)
			r.Patch("/v1/comments/{uid}", s.PatchComment)
			r.Delete("/v1/comments/{uid}", s.DeleteComment)
			r.Post("/v1/comments/{uid}/archive", s.ArchiveComment)
			r.Post("/v1/comments/{uid}/process", s.ProcessComment)

			// Chats REST endpoints
			r.Get("/v1/chats", s.ListChats)
			r.Post("/v1/chats", s.CreateChat)
			r.Get("/v1/chats/{uid}", s.GetChat)
			r.Put("/v1/chats/{uid}", s.UpdateChat)
			r.Patch("/v1/chats/{uid}", s.PatchChat)
			r.Delete("/v1/chats/{uid}", s.DeleteChat)
			r.Post("/v1/chats/{uid}/archive", s.ArchiveChat)
			r.Post("/v1/chats/{uid}/process", s.ProcessChat)

			// Chat Messages REST endpoints
			r.Get("/v1/chat_messages", s.ListChatMessages)
			r.Post("/v1/chat_messages", s.CreateChatMessage)
			r.Get("/v1/chat_messages/{uid}", s.GetChatMessage)
			r.Put("/v1/chat_messages/{uid}", s.UpdateChatMessage)
			r.Patch("/v1/chat_messages/{uid}", s.PatchChatMessage)
			r.Delete("/v1/chat_messages/{uid}", s.DeleteChatMessage)
			r.Post("/v1/chat_messages/{uid}/archive", s.ArchiveChatMessage)
			r.Post("/v1/chat_messages/{uid}/process", s.ProcessChatMessage)
		})

		// Wipe & state routes require auth + session, but NO epoch check
		// (otherwise you can't wipe when epoch is mismatched!)
		r.Group(func(r chi.Router) {
			r.Use(SessionRequired)

			r.Post("/v1/sync/wipe", s.WipeAccount)
			r.Get("/v1/sync/state", s.GetSyncState)
		})
	})

	log.Info().Msg("HTTP routes registered")
	return r
}
