package syncx

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Cursor represents a position in the sync stream
// Format: base64("<updated_at_ms>|<uuid>")
// Ensures lexicographically ordered, deterministic pagination
type Cursor struct {
	Ms  int64     // Unix milliseconds timestamp
	UID uuid.UUID // Entity UUID (for deterministic ordering within same timestamp)
}

// EncodeCursor creates a base64-encoded cursor string
// Returns empty string for zero-value cursor
func EncodeCursor(c Cursor) string {
	if c.Ms == 0 && c.UID == uuid.Nil {
		return ""
	}
	raw := fmt.Sprintf("%d|%s", c.Ms, c.UID.String())
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor parses a cursor string
// Returns zero-value cursor and false if invalid or empty
func DecodeCursor(s string) (Cursor, bool) {
	if s == "" {
		return Cursor{}, false
	}

	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, false
	}

	parts := strings.Split(string(b), "|")
	if len(parts) != 2 {
		return Cursor{}, false
	}

	ms, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return Cursor{}, false
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return Cursor{}, false
	}

	return Cursor{Ms: ms, UID: id}, true
}

// RFC3339 converts Unix milliseconds to RFC3339 timestamp string
func RFC3339(ms int64) string {
	return time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
}

// NowMs returns current Unix milliseconds timestamp (UTC)
func NowMs() int64 {
	return time.Now().UTC().UnixMilli()
}

// MsToTime converts Unix milliseconds to time.Time
func MsToTime(ms int64) time.Time {
	return time.UnixMilli(ms).UTC()
}
