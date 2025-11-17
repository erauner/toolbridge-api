package tools

import (
	"context"
	"encoding/json"
	"errors"
)

// Context attachment handlers
// NOTE: These use in-memory storage scoped to MCP session
// Attachments are lost when session expires (24h TTL)
// Future enhancement: persist to REST API when endpoints are available

func HandleAttachContext(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params AttachContextParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, NewToolError(ErrCodeInvalidParams, "Invalid parameters: "+err.Error(), nil)
	}
	if err := params.Validate(); err != nil {
		return nil, NewToolError(ErrCodeInvalidParams, err.Error(), nil)
	}

	uid, err := params.ParseUID()
	if err != nil {
		return nil, NewToolError(ErrCodeInvalidParams, "Invalid UID: "+err.Error(), nil)
	}

	// Add attachment to session
	attachment := Attachment{
		UID:   uid.String(),
		Kind:  params.EntityKind,
		Title: params.Title,
	}

	if err := tc.SessionManager.AddAttachment(tc.SessionID, attachment); err != nil {
		// Map known client errors to ErrCodeInvalidParams
		if errors.Is(err, ErrAttachmentAlreadyExists) || errors.Is(err, ErrAttachmentLimitExceeded) || errors.Is(err, ErrSessionNotFound) {
			return nil, NewToolError(ErrCodeInvalidParams, err.Error(), nil)
		}
		// Unknown/internal errors
		return nil, NewToolError(ErrCodeInternal, "Failed to attach context: "+err.Error(), nil)
	}

	tc.Logger.Info().
		Str("entityUID", uid.String()).
		Str("entityKind", params.EntityKind).
		Str("sessionID", tc.SessionID).
		Msg("Context attached to MCP session")

	return map[string]any{
		"success": true,
		"message": "Context attachment stored in MCP session (in-memory, non-persistent)",
		"attachment": map[string]any{
			"uid":   uid.String(),
			"kind":  params.EntityKind,
			"title": params.Title,
		},
	}, nil
}

func HandleDetachContext(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params DetachContextParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, NewToolError(ErrCodeInvalidParams, "Invalid parameters: "+err.Error(), nil)
	}
	if err := params.Validate(); err != nil {
		return nil, NewToolError(ErrCodeInvalidParams, err.Error(), nil)
	}

	uid, err := params.ParseUID()
	if err != nil {
		return nil, NewToolError(ErrCodeInvalidParams, "Invalid UID: "+err.Error(), nil)
	}

	// Remove attachment from session (by both UID and kind to target specific attachment)
	if err := tc.SessionManager.RemoveAttachment(tc.SessionID, uid.String(), params.EntityKind); err != nil {
		// Map known client errors to ErrCodeInvalidParams
		if errors.Is(err, ErrAttachmentNotFound) || errors.Is(err, ErrSessionNotFound) {
			return nil, NewToolError(ErrCodeInvalidParams, err.Error(), nil)
		}
		// Unknown/internal errors
		return nil, NewToolError(ErrCodeInternal, "Failed to detach context: "+err.Error(), nil)
	}

	tc.Logger.Info().
		Str("entityUID", uid.String()).
		Str("entityKind", params.EntityKind).
		Str("sessionID", tc.SessionID).
		Msg("Context detached from MCP session")

	return map[string]any{
		"success": true,
		"message": "Context attachment removed from MCP session",
	}, nil
}

func HandleListContext(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params ListContextParams
	if err := params.Validate(); err != nil {
		return nil, NewToolError(ErrCodeInvalidParams, err.Error(), nil)
	}

	// Retrieve attachments from session
	attachments, err := tc.SessionManager.ListAttachments(tc.SessionID)
	if err != nil {
		// Map session not found to client error
		if errors.Is(err, ErrSessionNotFound) {
			return nil, NewToolError(ErrCodeInvalidParams, err.Error(), nil)
		}
		return nil, NewToolError(ErrCodeInternal, "Failed to list context: "+err.Error(), nil)
	}

	tc.Logger.Info().
		Str("sessionID", tc.SessionID).
		Int("count", len(attachments)).
		Msg("Listed context attachments")

	return map[string]any{
		"attachments": attachments,
	}, nil
}

func HandleClearContext(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params ClearContextParams
	if err := params.Validate(); err != nil {
		return nil, NewToolError(ErrCodeInvalidParams, err.Error(), nil)
	}

	// Clear all attachments from session
	if err := tc.SessionManager.ClearAttachments(tc.SessionID); err != nil {
		// Map session not found to client error
		if errors.Is(err, ErrSessionNotFound) {
			return nil, NewToolError(ErrCodeInvalidParams, err.Error(), nil)
		}
		return nil, NewToolError(ErrCodeInternal, "Failed to clear context: "+err.Error(), nil)
	}

	tc.Logger.Info().
		Str("sessionID", tc.SessionID).
		Msg("Cleared all context attachments")

	return map[string]any{
		"success": true,
		"message": "All context attachments cleared from MCP session",
	}, nil
}
