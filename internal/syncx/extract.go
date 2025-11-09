package syncx

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// Extracted contains parsed sync metadata from client JSON
type Extracted struct {
	UID         uuid.UUID
	UpdatedAtMs int64
	DeletedAtMs *int64
	Version     int
	ParentType  string     // for comments
	ParentUID   *uuid.UUID // for comments
	ChatUID     *uuid.UUID // for chat_message
}

// GetString safely extracts a string value from a map
func GetString(m map[string]any, k string) (string, bool) {
	if v, ok := m[k]; ok {
		if s, ok2 := v.(string); ok2 {
			return s, true
		}
	}
	return "", false
}

// GetMap safely extracts a nested map from a map
// Handles both map[string]any and map[string]interface{} (protobuf compatibility)
func GetMap(m map[string]any, k string) (map[string]any, bool) {
	if v, ok := m[k]; ok {
		// Try map[string]any first
		if mm, ok2 := v.(map[string]any); ok2 {
			return mm, true
		}
		// Try map[string]interface{} (protobuf Struct.AsMap() returns this)
		if mm, ok2 := v.(map[string]interface{}); ok2 {
			// Convert to map[string]any
			converted := make(map[string]any, len(mm))
			for key, val := range mm {
				converted[key] = val
			}
			return converted, true
		}
	}
	return nil, false
}

// ParseUUID parses a UUID string
func ParseUUID(s string) (uuid.UUID, bool) {
	if s == "" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(s)
	return id, err == nil
}

// ParseTimeToMs converts various time formats to Unix milliseconds
// Accepts: RFC3339, numeric milliseconds (as string), empty (returns 0)
func ParseTimeToMs(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}

	// Try RFC3339 first
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC().UnixMilli(), true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC().UnixMilli(), true
	}

	// Try numeric milliseconds
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		return ms, true
	}

	return 0, false
}

// ExtractCommon parses common sync metadata from client JSON
// Tolerant of various field naming conventions (updatedTs, updatedAt, updateTime)
func ExtractCommon(item map[string]any) (Extracted, error) {
	var out Extracted

	// 1. Extract UID (required)
	uidStr, _ := GetString(item, "uid")
	id, ok := ParseUUID(uidStr)
	if !ok {
		return out, errors.New("missing or invalid uid")
	}
	out.UID = id

	// 2. Extract updated timestamp (try multiple field names)
	var updMs int64
	if s, ok := GetString(item, "updatedTs"); ok {
		if ms, ok2 := ParseTimeToMs(s); ok2 {
			updMs = ms
		}
	}
	if updMs == 0 {
		if s, ok := GetString(item, "updatedAt"); ok {
			if ms, ok2 := ParseTimeToMs(s); ok2 {
				updMs = ms
			}
		}
	}
	if updMs == 0 {
		if s, ok := GetString(item, "updateTime"); ok {
			if ms, ok2 := ParseTimeToMs(s); ok2 {
				updMs = ms
			}
		}
	}
	if updMs == 0 {
		// Fallback to server time (but client should always provide timestamp)
		updMs = NowMs()
	}
	out.UpdatedAtMs = updMs

	// 3. Extract sync metadata (version, isDeleted, deletedAt)
	if sync, ok := GetMap(item, "sync"); ok {
		// Version
		if v, ok := sync["version"].(float64); ok {
			out.Version = int(v)
		}

		// Deletion flag + timestamp
		if del, ok := sync["isDeleted"].(bool); ok && del {
			if ds, ok := GetString(sync, "deletedAt"); ok {
				if ms, ok2 := ParseTimeToMs(ds); ok2 {
					out.DeletedAtMs = &ms
				}
			}
			// If deleted but no deletedAt timestamp, use updatedAtMs
			if out.DeletedAtMs == nil {
				ms := updMs
				out.DeletedAtMs = &ms
			}
		}
	}

	// Default version to 1 if not specified
	if out.Version == 0 {
		out.Version = 1
	}

	return out, nil
}

// ExtractComment adds comment-specific fields (parentType, parentUid)
func ExtractComment(item map[string]any) (Extracted, error) {
	ext, err := ExtractCommon(item)
	if err != nil {
		return ext, err
	}

	// Extract parent type
	if pt, ok := GetString(item, "parentType"); ok {
		ext.ParentType = pt
	} else {
		return ext, errors.New("missing parentType")
	}

	// Extract parent UID
	if pu, ok := GetString(item, "parentUid"); ok {
		if puid, ok2 := ParseUUID(pu); ok2 {
			ext.ParentUID = &puid
		} else {
			return ext, errors.New("invalid parentUid")
		}
	} else {
		return ext, errors.New("missing parentUid")
	}

	return ext, nil
}

// ExtractChatMessage adds chat message specific fields (chatUid)
func ExtractChatMessage(item map[string]any) (Extracted, error) {
	ext, err := ExtractCommon(item)
	if err != nil {
		return ext, err
	}

	// Extract chat UID
	if cu, ok := GetString(item, "chatUid"); ok {
		if cuid, ok2 := ParseUUID(cu); ok2 {
			ext.ChatUID = &cuid
		} else {
			return ext, fmt.Errorf("invalid chatUid: %s", cu)
		}
	} else {
		return ext, errors.New("missing chatUid")
	}

	return ext, nil
}
