package httpapi

import (
	"net/http"
	"time"
)

// ServerInfo represents the server's capabilities and configuration
type ServerInfo struct {
	APIVersion       string                       `json:"apiVersion"`
	ServerTime       string                       `json:"serverTime"`
	Entities         map[string]EntityCapability  `json:"entities"`
	RecommendedBatch int                          `json:"recommendedBatch,omitempty"` // Deprecated: use Hints.RecommendedBatch
	Locking          LockingCapability            `json:"locking"`
	MinClientVersion string                       `json:"minClientVersion"`
	RateLimit        *RateLimitInfo               `json:"rateLimit,omitempty"`
	Hints            *SyncHints                   `json:"hints,omitempty"`
}

// RateLimitInfo describes the server's rate limiting policy
type RateLimitInfo struct {
	WindowSeconds int `json:"windowSeconds"` // e.g. 60
	MaxRequests   int `json:"maxRequests"`   // per window
	Burst         int `json:"burst"`         // token bucket size
}

// SyncHints provides recommendations for client behavior
type SyncHints struct {
	RecommendedBatch int `json:"recommendedBatch"` // safe batch size
	BackoffMsOn429   int `json:"backoffMsOn429"`   // default backoff if Retry-After missing
}

// EntityCapability describes capabilities for a specific entity type
type EntityCapability struct {
	MaxLimit int  `json:"maxLimit"`
	Enabled  bool `json:"enabled,omitempty"` // deprecated, kept for backward compatibility
	Push     bool `json:"push"`              // push operations enabled
	Pull     bool `json:"pull"`              // pull operations enabled
}

// LockingCapability describes sync locking/session support
type LockingCapability struct {
	Supported bool   `json:"supported"`
	Mode      string `json:"mode"` // "session" or "none"
}

// Info handles GET /v1/sync/info
// Returns server capabilities, API version, and supported features
// This endpoint can be called without authentication to allow capability discovery
func (s *Server) Info(w http.ResponseWriter, r *http.Request) {
	info := ServerInfo{
		APIVersion: "1.1",
		ServerTime: time.Now().UTC().Format(time.RFC3339Nano),
		Entities: map[string]EntityCapability{
			"notes": {
				MaxLimit: 1000,
				Push:     true,
				Pull:     true,
			},
			"tasks": {
				MaxLimit: 1000,
				Push:     true,
				Pull:     true,
			},
			"comments": {
				MaxLimit: 1000,
				Push:     true,
				Pull:     true,
			},
			"chats": {
				MaxLimit: 1000,
				Push:     true,
				Pull:     true,
			},
			"chat_messages": {
				MaxLimit: 1000,
				Push:     true,
				Pull:     true,
			},
			"task_lists": {
				MaxLimit: 1000,
				Push:     true,
				Pull:     true,
			},
			"task_list_categories": {
				MaxLimit: 1000,
				Push:     true,
				Pull:     true,
			},
		},
		Locking: LockingCapability{
			Supported: true,
			Mode:      "session",
		},
		MinClientVersion: "0.1.0",
		RateLimit:        &s.RateLimitConfig,
		Hints: &SyncHints{
			RecommendedBatch: 500,
			BackoffMsOn429:   1500,
		},
	}

	writeJSON(w, http.StatusOK, info)
}
