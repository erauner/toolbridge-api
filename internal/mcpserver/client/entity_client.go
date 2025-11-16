package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/google/uuid"
)

// EntityClient provides generic CRUD operations for a specific entity type
// Supports notes, tasks, comments, chats, and chat_messages
// Reference: internal/httpapi/rest_items.go (server-side)
type EntityClient struct {
	http     *HTTPClient
	basePath string // e.g., "/v1/notes"
}

// NewEntityClient creates a new entity client for a specific entity type
// entityType should be one of: "notes", "tasks", "comments", "chats", "chat_messages"
func NewEntityClient(httpClient *HTTPClient, entityType string) *EntityClient {
	return &EntityClient{
		http:     httpClient,
		basePath: fmt.Sprintf("/v1/%s", entityType),
	}
}

// List fetches entities with cursor-based pagination
// Reference: internal/httpapi/rest_items.go:ListNotes, ListTasks, etc.
func (c *EntityClient) List(ctx context.Context, opts ListOpts) (*RESTListResponse, error) {
	params := url.Values{}
	if opts.Cursor != "" {
		params.Set("cursor", opts.Cursor)
	}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.IncludeDeleted {
		params.Set("includeDeleted", "true")
	}

	reqURL := fmt.Sprintf("%s%s?%s", c.http.baseURL, c.basePath, params.Encode())
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list failed with status %d", resp.StatusCode)
	}

	var listResp RESTListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("failed to decode list response: %w", err)
	}

	return &listResp, nil
}

// Get retrieves a single entity by UID
// Returns ErrNotFound if entity doesn't exist
// Returns ErrDeleted if entity is deleted (unless includeDeleted=true)
// Reference: internal/httpapi/rest_items.go:GetNote, GetTask, etc.
func (c *EntityClient) Get(ctx context.Context, uid uuid.UUID, includeDeleted bool) (*RESTItem, error) {
	reqURL := fmt.Sprintf("%s%s/%s", c.http.baseURL, c.basePath, uid.String())
	if includeDeleted {
		reqURL += "?includeDeleted=true"
	}

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return nil, ErrNotFound{UID: uid.String()}
	case http.StatusGone:
		return nil, ErrDeleted{UID: uid.String()}
	case http.StatusOK:
		// Success - decode response
	default:
		return nil, fmt.Errorf("get failed with status %d", resp.StatusCode)
	}

	var item RESTItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, fmt.Errorf("failed to decode item: %w", err)
	}

	return &item, nil
}

// Create creates a new entity
// If payload doesn't contain "uid", server will generate one
// Returns the created entity with server-assigned UID and version
// Reference: internal/httpapi/rest_items.go:CreateNote, CreateTask, etc.
func (c *EntityClient) Create(ctx context.Context, payload map[string]any) (*RESTItem, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	reqURL := fmt.Sprintf("%s%s", c.http.baseURL, c.basePath)
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("create failed with status %d", resp.StatusCode)
	}

	var item RESTItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, fmt.Errorf("failed to decode created item: %w", err)
	}

	return &item, nil
}

// Update performs a full replacement (PUT)
// Supports optimistic locking via version parameter
// If version is provided, server checks for version mismatch (409 Conflict)
// Reference: internal/httpapi/rest_items.go:UpdateNote, UpdateTask, etc.
func (c *EntityClient) Update(ctx context.Context, uid uuid.UUID, payload map[string]any, version *int) (*RESTItem, error) {
	// Ensure UID is in payload
	payload["uid"] = uid.String()

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	reqURL := fmt.Sprintf("%s%s/%s", c.http.baseURL, c.basePath, uid.String())
	req, err := http.NewRequestWithContext(ctx, "PUT", reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Optimistic locking with If-Match header
	// Server expects version as ETag format (can be quoted or unquoted)
	if version != nil {
		req.Header.Set("If-Match", strconv.Itoa(*version))
	}

	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusConflict:
		// Version mismatch or epoch mismatch
		// Try to parse response to distinguish
		var errResp struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(resp.Body).Decode(&errResp) == nil && errResp.Error != "epoch_mismatch" {
			return nil, ErrVersionMismatch{Expected: *version}
		}
		// Epoch mismatch - already handled by HTTPClient retry logic
		return nil, fmt.Errorf("update failed with conflict (epoch mismatch)")

	case http.StatusOK:
		// Success - decode response
	default:
		return nil, fmt.Errorf("update failed with status %d", resp.StatusCode)
	}

	var item RESTItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, fmt.Errorf("failed to decode updated item: %w", err)
	}

	return &item, nil
}

// Patch performs a partial update (PATCH)
// Only fields present in partial will be updated
// Server will merge partial into existing entity
// Reference: internal/httpapi/rest_items.go:PatchNote, PatchTask, etc.
func (c *EntityClient) Patch(ctx context.Context, uid uuid.UUID, partial map[string]any) (*RESTItem, error) {
	body, err := json.Marshal(partial)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal partial: %w", err)
	}

	reqURL := fmt.Sprintf("%s%s/%s", c.http.baseURL, c.basePath, uid.String())
	req, err := http.NewRequestWithContext(ctx, "PATCH", reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("patch failed with status %d", resp.StatusCode)
	}

	var item RESTItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, fmt.Errorf("failed to decode patched item: %w", err)
	}

	return &item, nil
}

// Delete performs a soft delete on an entity
// Entity will be marked as deleted (tombstone) but not removed from database
// Reference: internal/httpapi/rest_items.go:DeleteNote, DeleteTask, etc.
func (c *EntityClient) Delete(ctx context.Context, uid uuid.UUID) error {
	reqURL := fmt.Sprintf("%s%s/%s", c.http.baseURL, c.basePath, uid.String())
	req, err := http.NewRequestWithContext(ctx, "DELETE", reqURL, nil)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("delete failed with status %d", resp.StatusCode)
	}

	return nil
}

// Archive sets an entity's status to "archived"
// For notes/tasks/comments: sets status="archived"
// For chats/messages: sets archived=true
// Reference: internal/httpapi/rest_items.go:ArchiveNote, etc.
func (c *EntityClient) Archive(ctx context.Context, uid uuid.UUID) (*RESTItem, error) {
	reqURL := fmt.Sprintf("%s%s/%s/archive", c.http.baseURL, c.basePath, uid.String())
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("archive failed with status %d", resp.StatusCode)
	}

	var item RESTItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, fmt.Errorf("failed to decode archived item: %w", err)
	}

	return &item, nil
}

// Process executes a server-side action on an entity
// Supported actions vary by entity type:
// - Notes: pin, unpin, archive, unarchive
// - Tasks: start, complete, reopen
// - Comments: resolve, reopen
// - Chats: resolve, reopen
// - Chat Messages: mark_read, mark_delivered
//
// Reference: internal/httpapi/rest_items.go:ProcessNote, ProcessTask, etc.
func (c *EntityClient) Process(ctx context.Context, uid uuid.UUID, action string, metadata map[string]any) (*RESTItem, error) {
	payload := map[string]any{
		"action": action,
	}
	if metadata != nil {
		payload["metadata"] = metadata
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal action payload: %w", err)
	}

	reqURL := fmt.Sprintf("%s%s/%s/process", c.http.baseURL, c.basePath, uid.String())
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("process action failed with status %d", resp.StatusCode)
	}

	var item RESTItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, fmt.Errorf("failed to decode processed item: %w", err)
	}

	return &item, nil
}
