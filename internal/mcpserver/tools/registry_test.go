package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRegistry_Call_MCPContentFormat(t *testing.T) {
	// Test that Registry.Call wraps handler results in MCP content format
	registry := NewRegistry()

	// Register a simple test tool
	registry.MustRegister(ToolDefinition{
		Name:        "test.echo",
		Description: "Echo test tool",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, func(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
		// Return a simple object
		return map[string]any{
			"message": "hello world",
			"count":   42,
		}, nil
	})

	// Call the tool
	result, err := registry.Call(context.Background(), nil, CallRequest{
		Name:      "test.echo",
		Arguments: json.RawMessage(`{}`),
	})

	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	// Verify result is wrapped in CallResult format
	callResult, ok := result.(CallResult)
	if !ok {
		t.Fatalf("Expected CallResult, got %T", result)
	}

	// Verify content structure
	if len(callResult.Content) != 1 {
		t.Fatalf("Expected 1 content block, got %d", len(callResult.Content))
	}

	contentBlock := callResult.Content[0]
	if contentBlock.Type != "text" {
		t.Errorf("Expected content type 'text', got '%s'", contentBlock.Type)
	}

	// Verify the text is valid JSON
	var decoded map[string]any
	if err := json.Unmarshal([]byte(contentBlock.Text), &decoded); err != nil {
		t.Fatalf("Content text is not valid JSON: %v", err)
	}

	// Verify the original data is intact
	if decoded["message"] != "hello world" {
		t.Errorf("Expected message 'hello world', got '%v'", decoded["message"])
	}

	// JSON numbers are decoded as float64
	if count, ok := decoded["count"].(float64); !ok || count != 42 {
		t.Errorf("Expected count 42, got %v", decoded["count"])
	}

	// Verify IsError is false
	if callResult.IsError {
		t.Error("Expected IsError to be false")
	}
}

func TestRegistry_Call_ToolNotFound(t *testing.T) {
	registry := NewRegistry()

	_, err := registry.Call(context.Background(), nil, CallRequest{
		Name:      "nonexistent.tool",
		Arguments: json.RawMessage(`{}`),
	})

	if err == nil {
		t.Fatal("Expected error for nonexistent tool")
	}

	toolErr, ok := err.(*ToolError)
	if !ok {
		t.Fatalf("Expected ToolError, got %T", err)
	}

	if toolErr.Code != ErrCodeMethodNotFound {
		t.Errorf("Expected error code METHOD_NOT_FOUND, got %s", toolErr.Code)
	}
}

func TestRegistry_Call_HandlerError(t *testing.T) {
	registry := NewRegistry()

	// Register a tool that returns an error
	registry.MustRegister(ToolDefinition{
		Name:        "test.fail",
		Description: "Failing test tool",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, func(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
		return nil, NewToolError(ErrCodeInvalidParams, "Invalid input", map[string]any{
			"field": "test",
		})
	})

	_, err := registry.Call(context.Background(), nil, CallRequest{
		Name:      "test.fail",
		Arguments: json.RawMessage(`{}`),
	})

	if err == nil {
		t.Fatal("Expected error from handler")
	}

	toolErr, ok := err.(*ToolError)
	if !ok {
		t.Fatalf("Expected ToolError, got %T", err)
	}

	if toolErr.Code != ErrCodeInvalidParams {
		t.Errorf("Expected error code INVALID_PARAMS, got %s", toolErr.Code)
	}

	if toolErr.Data == nil {
		t.Error("Expected error data to be preserved")
	}

	if toolErr.Data["field"] != "test" {
		t.Errorf("Expected data field 'test', got %v", toolErr.Data["field"])
	}
}

func TestRegistry_List(t *testing.T) {
	registry := NewRegistry()

	// Register multiple tools
	registry.MustRegister(ToolDefinition{
		Name:        "test.one",
		Description: "First test tool",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
		return nil, nil
	})

	registry.MustRegister(ToolDefinition{
		Name:        "test.two",
		Description: "Second test tool",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
		return nil, nil
	})

	descriptors := registry.List()

	if len(descriptors) != 2 {
		t.Fatalf("Expected 2 tools, got %d", len(descriptors))
	}

	// Verify order is preserved
	if descriptors[0].Name != "test.one" {
		t.Errorf("Expected first tool to be 'test.one', got '%s'", descriptors[0].Name)
	}

	if descriptors[1].Name != "test.two" {
		t.Errorf("Expected second tool to be 'test.two', got '%s'", descriptors[1].Name)
	}

	// Verify structure
	if descriptors[0].Description != "First test tool" {
		t.Errorf("Expected description 'First test tool', got '%s'", descriptors[0].Description)
	}

	if descriptors[0].InputSchema == nil {
		t.Error("Expected InputSchema to be present")
	}
}

func TestRegistry_Register_DuplicateName(t *testing.T) {
	registry := NewRegistry()

	dummyHandler := func(ctx context.Context, tc *ToolContext, raw json.RawMessage) (interface{}, error) {
		return nil, nil
	}

	err := registry.Register(ToolDefinition{
		Name:        "test.tool",
		Description: "Test tool",
		InputSchema: map[string]any{"type": "object"},
	}, dummyHandler)

	if err != nil {
		t.Fatalf("First registration failed: %v", err)
	}

	// Try to register same name again
	err = registry.Register(ToolDefinition{
		Name:        "test.tool",
		Description: "Duplicate tool",
		InputSchema: map[string]any{"type": "object"},
	}, dummyHandler)

	if err == nil {
		t.Fatal("Expected error for duplicate registration")
	}
}
