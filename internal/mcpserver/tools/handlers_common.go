package tools

import (
	"context"
	"encoding/json"

	"github.com/erauner12/toolbridge-api/internal/mcpserver/client"
)

// Generic handlers for comments, chats, and chat_messages

// Comments handlers

func HandleListComments(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params ListCommentsParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, NewToolError(ErrCodeInvalidParams, "Invalid parameters: "+err.Error(), nil)
		}
	}
	if err := params.Validate(); err != nil {
		return nil, NewToolError(ErrCodeInvalidParams, err.Error(), nil)
	}

	commentsClient, err := tc.GetClient("comments")
	if err != nil {
		return nil, err
	}

	opts := client.ListOpts{
		Limit:          100,
		IncludeDeleted: params.IncludeDeleted,
	}
	if params.Cursor != nil {
		opts.Cursor = *params.Cursor
	}
	if params.Limit != nil {
		opts.Limit = *params.Limit
	}

	result, err := commentsClient.List(ctx, opts)
	if err != nil {
		return nil, WrapClientError(err)
	}

	return result, nil
}

func HandleGetComment(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params GetCommentParams
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

	commentsClient, err := tc.GetClient("comments")
	if err != nil {
		return nil, err
	}

	item, err := commentsClient.Get(ctx, uid, params.IncludeDeleted)
	if err != nil {
		return nil, WrapClientError(err)
	}

	return item, nil
}

func HandleCreateComment(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params CreateCommentParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, NewToolError(ErrCodeInvalidParams, "Invalid parameters: "+err.Error(), nil)
	}
	if err := params.Validate(); err != nil {
		return nil, NewToolError(ErrCodeInvalidParams, err.Error(), nil)
	}

	commentsClient, err := tc.GetClient("comments")
	if err != nil {
		return nil, err
	}

	// If client provided UID, add it to payload
	if params.UID != nil {
		uid, err := params.ParseUID()
		if err != nil {
			return nil, NewToolError(ErrCodeInvalidParams, "Invalid UID: "+err.Error(), nil)
		}
		if uid != nil {
			params.Payload["uid"] = uid.String()
		}
	}

	item, err := commentsClient.Create(ctx, params.Payload)
	if err != nil {
		return nil, WrapClientError(err)
	}

	return item, nil
}

// Chats handlers

func HandleListChats(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params ListChatsParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, NewToolError(ErrCodeInvalidParams, "Invalid parameters: "+err.Error(), nil)
		}
	}
	if err := params.Validate(); err != nil {
		return nil, NewToolError(ErrCodeInvalidParams, err.Error(), nil)
	}

	chatsClient, err := tc.GetClient("chats")
	if err != nil {
		return nil, err
	}

	opts := client.ListOpts{
		Limit:          100,
		IncludeDeleted: params.IncludeDeleted,
	}
	if params.Cursor != nil {
		opts.Cursor = *params.Cursor
	}
	if params.Limit != nil {
		opts.Limit = *params.Limit
	}

	result, err := chatsClient.List(ctx, opts)
	if err != nil {
		return nil, WrapClientError(err)
	}

	return result, nil
}

func HandleGetChat(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params GetChatParams
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

	chatsClient, err := tc.GetClient("chats")
	if err != nil {
		return nil, err
	}

	item, err := chatsClient.Get(ctx, uid, params.IncludeDeleted)
	if err != nil {
		return nil, WrapClientError(err)
	}

	return item, nil
}

func HandleCreateChat(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params CreateChatParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, NewToolError(ErrCodeInvalidParams, "Invalid parameters: "+err.Error(), nil)
	}
	if err := params.Validate(); err != nil {
		return nil, NewToolError(ErrCodeInvalidParams, err.Error(), nil)
	}

	chatsClient, err := tc.GetClient("chats")
	if err != nil {
		return nil, err
	}

	// If client provided UID, add it to payload
	if params.UID != nil {
		uid, err := params.ParseUID()
		if err != nil {
			return nil, NewToolError(ErrCodeInvalidParams, "Invalid UID: "+err.Error(), nil)
		}
		if uid != nil {
			params.Payload["uid"] = uid.String()
		}
	}

	item, err := chatsClient.Create(ctx, params.Payload)
	if err != nil {
		return nil, WrapClientError(err)
	}

	return item, nil
}

// ChatMessages handlers

func HandleListChatMessages(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params ListChatMessagesParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, NewToolError(ErrCodeInvalidParams, "Invalid parameters: "+err.Error(), nil)
		}
	}
	if err := params.Validate(); err != nil {
		return nil, NewToolError(ErrCodeInvalidParams, err.Error(), nil)
	}

	chatMessagesClient, err := tc.GetClient("chat_messages")
	if err != nil {
		return nil, err
	}

	opts := client.ListOpts{
		Limit:          100,
		IncludeDeleted: params.IncludeDeleted,
	}
	if params.Cursor != nil {
		opts.Cursor = *params.Cursor
	}
	if params.Limit != nil {
		opts.Limit = *params.Limit
	}

	result, err := chatMessagesClient.List(ctx, opts)
	if err != nil {
		return nil, WrapClientError(err)
	}

	return result, nil
}

func HandleGetChatMessage(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params GetChatMessageParams
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

	chatMessagesClient, err := tc.GetClient("chat_messages")
	if err != nil {
		return nil, err
	}

	item, err := chatMessagesClient.Get(ctx, uid, params.IncludeDeleted)
	if err != nil {
		return nil, WrapClientError(err)
	}

	return item, nil
}

func HandleCreateChatMessage(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params CreateChatMessageParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, NewToolError(ErrCodeInvalidParams, "Invalid parameters: "+err.Error(), nil)
	}
	if err := params.Validate(); err != nil {
		return nil, NewToolError(ErrCodeInvalidParams, err.Error(), nil)
	}

	chatMessagesClient, err := tc.GetClient("chat_messages")
	if err != nil {
		return nil, err
	}

	// If client provided UID, add it to payload
	if params.UID != nil {
		uid, err := params.ParseUID()
		if err != nil {
			return nil, NewToolError(ErrCodeInvalidParams, "Invalid UID: "+err.Error(), nil)
		}
		if uid != nil {
			params.Payload["uid"] = uid.String()
		}
	}

	item, err := chatMessagesClient.Create(ctx, params.Payload)
	if err != nil {
		return nil, WrapClientError(err)
	}

	return item, nil
}

// Generic update/patch/delete/archive handlers used by multiple entity types

func HandleGenericUpdate(entityType string) Handler {
	return func(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
		var params GenericUpdateParams
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

		entityClient, err := tc.GetClient(entityType)
		if err != nil {
			return nil, err
		}

		item, err := entityClient.Update(ctx, uid, params.Payload, params.Version)
		if err != nil {
			return nil, WrapClientError(err)
		}

		return item, nil
	}
}

func HandleGenericPatch(entityType string) Handler {
	return func(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
		var params GenericPatchParams
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

		entityClient, err := tc.GetClient(entityType)
		if err != nil {
			return nil, err
		}

		item, err := entityClient.Patch(ctx, uid, params.Partial)
		if err != nil {
			return nil, WrapClientError(err)
		}

		return item, nil
	}
}

func HandleGenericDelete(entityType string) Handler {
	return func(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
		var params GenericDeleteParams
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

		entityClient, err := tc.GetClient(entityType)
		if err != nil {
			return nil, err
		}

		if err := entityClient.Delete(ctx, uid); err != nil {
			return nil, WrapClientError(err)
		}

		return map[string]any{"success": true}, nil
	}
}

func HandleGenericArchive(entityType string) Handler {
	return func(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
		var params GenericArchiveParams
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

		entityClient, err := tc.GetClient(entityType)
		if err != nil {
			return nil, err
		}

		item, err := entityClient.Archive(ctx, uid)
		if err != nil {
			return nil, WrapClientError(err)
		}

		return item, nil
	}
}

func HandleGenericProcess(entityType string, allowedActions map[string]bool) Handler {
	return func(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
		var params GenericProcessParams
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, NewToolError(ErrCodeInvalidParams, "Invalid parameters: "+err.Error(), nil)
		}
		if err := params.Validate(allowedActions); err != nil {
			return nil, NewToolError(ErrCodeInvalidParams, err.Error(), nil)
		}

		uid, err := params.ParseUID()
		if err != nil {
			return nil, NewToolError(ErrCodeInvalidParams, "Invalid UID: "+err.Error(), nil)
		}

		entityClient, err := tc.GetClient(entityType)
		if err != nil {
			return nil, err
		}

		item, err := entityClient.Process(ctx, uid, params.Action, params.Metadata)
		if err != nil {
			return nil, WrapClientError(err)
		}

		return item, nil
	}
}
