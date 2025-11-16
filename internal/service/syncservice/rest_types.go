package syncservice

import "fmt"

// RESTItem represents a single entity with sync metadata exposed
type RESTItem struct {
	UID       string         `json:"uid"`
	Version   int            `json:"version"`
	UpdatedAt string         `json:"updatedAt"`
	DeletedAt *string        `json:"deletedAt,omitempty"`
	Payload   map[string]any `json:"payload"`
}

// RESTListResponse represents paginated list response
type RESTListResponse struct {
	Items      []RESTItem `json:"items"`
	NextCursor *string    `json:"nextCursor,omitempty"`
}

// MutationOpts configures REST mutation behavior
type MutationOpts struct {
	EnforceVersion   bool   // If true, check version matches before updating
	ExpectedVersion  int    // Expected version for optimistic locking
	ForceTimestampMs *int64 // Override timestamp (for testing)
	SetDeleted       bool   // Mark as deleted
}

// VersionMismatchError indicates optimistic locking failure
type VersionMismatchError struct {
	Expected int
	Actual   int
}

func (e *VersionMismatchError) Error() string {
	return fmt.Sprintf("version mismatch: expected %d, actual %d", e.Expected, e.Actual)
}

// MutationError wraps mutation failures
type MutationError struct {
	Message string
}

func (e *MutationError) Error() string {
	return e.Message
}
