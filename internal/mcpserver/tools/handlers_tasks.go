package tools

import (
	"context"
	"encoding/json"

	"github.com/erauner12/toolbridge-api/internal/mcpserver/client"
)

// Tasks tool handlers

func HandleListTasks(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params ListTasksParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, NewToolError(ErrCodeInvalidParams, "Invalid parameters: "+err.Error(), nil)
		}
	}
	if err := params.Validate(); err != nil {
		return nil, NewToolError(ErrCodeInvalidParams, err.Error(), nil)
	}

	tasksClient, err := tc.GetClient("tasks")
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

	result, err := tasksClient.List(ctx, opts)
	if err != nil {
		return nil, WrapClientError(err)
	}

	return result, nil
}

func HandleGetTask(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params GetTaskParams
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

	tasksClient, err := tc.GetClient("tasks")
	if err != nil {
		return nil, err
	}

	item, err := tasksClient.Get(ctx, uid, params.IncludeDeleted)
	if err != nil {
		return nil, WrapClientError(err)
	}

	return item, nil
}

func HandleCreateTask(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params CreateTaskParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, NewToolError(ErrCodeInvalidParams, "Invalid parameters: "+err.Error(), nil)
	}
	if err := params.Validate(); err != nil {
		return nil, NewToolError(ErrCodeInvalidParams, err.Error(), nil)
	}

	tasksClient, err := tc.GetClient("tasks")
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

	item, err := tasksClient.Create(ctx, params.Payload)
	if err != nil {
		return nil, WrapClientError(err)
	}

	return item, nil
}

func HandleUpdateTask(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params UpdateTaskParams
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

	tasksClient, err := tc.GetClient("tasks")
	if err != nil {
		return nil, err
	}

	item, err := tasksClient.Update(ctx, uid, params.Payload, params.Version)
	if err != nil {
		return nil, WrapClientError(err)
	}

	return item, nil
}

func HandlePatchTask(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params PatchTaskParams
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

	tasksClient, err := tc.GetClient("tasks")
	if err != nil {
		return nil, err
	}

	item, err := tasksClient.Patch(ctx, uid, params.Partial)
	if err != nil {
		return nil, WrapClientError(err)
	}

	return item, nil
}

func HandleDeleteTask(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params DeleteTaskParams
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

	tasksClient, err := tc.GetClient("tasks")
	if err != nil {
		return nil, err
	}

	if err := tasksClient.Delete(ctx, uid); err != nil {
		return nil, WrapClientError(err)
	}

	return map[string]any{"success": true}, nil
}

func HandleArchiveTask(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params ArchiveTaskParams
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

	tasksClient, err := tc.GetClient("tasks")
	if err != nil {
		return nil, err
	}

	item, err := tasksClient.Archive(ctx, uid)
	if err != nil {
		return nil, WrapClientError(err)
	}

	return item, nil
}

func HandleProcessTask(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
	var params ProcessTaskParams
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

	tasksClient, err := tc.GetClient("tasks")
	if err != nil {
		return nil, err
	}

	item, err := tasksClient.Process(ctx, uid, params.Action, params.Metadata)
	if err != nil {
		return nil, WrapClientError(err)
	}

	return item, nil
}
