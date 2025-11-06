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
	RecommendedBatch int                          `json:"recommendedBatch"`
	Locking          LockingCapability            `json:"locking"`
	MinClientVersion string                       `json:"minClientVersion"`
}

// EntityCapability describes capabilities for a specific entity type
type EntityCapability struct {
	MaxLimit int  `json:"maxLimit"`
	Enabled  bool `json:"enabled"`
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
		APIVersion: "1.0",
		ServerTime: time.Now().UTC().Format(time.RFC3339Nano),
		Entities: map[string]EntityCapability{
			"notes": {
				MaxLimit: 1000,
				Enabled:  true,
			},
			"tasks": {
				MaxLimit: 1000,
				Enabled:  true,
			},
			"comments": {
				MaxLimit: 1000,
				Enabled:  true,
			},
			"chats": {
				MaxLimit: 1000,
				Enabled:  true,
			},
			"chat_messages": {
				MaxLimit: 1000,
				Enabled:  true,
			},
		},
		RecommendedBatch: 500,
		Locking: LockingCapability{
			Supported: true,
			Mode:      "session",
		},
		MinClientVersion: "0.1.0",
	}

	writeJSON(w, http.StatusOK, info)
}
