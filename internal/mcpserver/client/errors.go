package client

import "fmt"

// ErrNotFound indicates an entity was not found (404)
type ErrNotFound struct {
	UID string
}

func (e ErrNotFound) Error() string {
	return fmt.Sprintf("entity %s not found", e.UID)
}

// ErrDeleted indicates an entity is deleted (410 Gone)
type ErrDeleted struct {
	UID string
}

func (e ErrDeleted) Error() string {
	return fmt.Sprintf("entity %s is deleted", e.UID)
}

// ErrVersionMismatch indicates optimistic locking failure (409 Conflict)
type ErrVersionMismatch struct {
	Expected int
	Actual   int
}

func (e ErrVersionMismatch) Error() string {
	if e.Expected > 0 {
		return fmt.Sprintf("version mismatch: expected %d, got %d", e.Expected, e.Actual)
	}
	return "version mismatch (optimistic locking failed)"
}

// ErrEpochMismatch indicates client epoch is behind server (409 Conflict)
type ErrEpochMismatch struct {
	ClientEpoch int
	ServerEpoch int
}

func (e ErrEpochMismatch) Error() string {
	return fmt.Sprintf("epoch mismatch: client=%d, server=%d (data reset required)", e.ClientEpoch, e.ServerEpoch)
}

// ErrRateLimited indicates too many requests (429)
type ErrRateLimited struct {
	RetryAfter int // seconds
}

func (e ErrRateLimited) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("rate limited (retry after %d seconds)", e.RetryAfter)
	}
	return "rate limited"
}
