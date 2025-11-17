package tools

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/erauner12/toolbridge-api/internal/mcpserver/client"
)

func TestWrapClientError_NotFound(t *testing.T) {
	clientErr := client.ErrNotFound{UID: "abc-123"}
	toolErr := WrapClientError(clientErr)

	if toolErr == nil {
		t.Fatal("Expected ToolError, got nil")
	}

	te, ok := toolErr.(*ToolError)
	if !ok {
		t.Fatalf("Expected *ToolError, got %T", toolErr)
	}

	if te.Code != ErrCodeNotFound {
		t.Errorf("Expected code NOT_FOUND, got %s", te.Code)
	}

	if te.Message == "" {
		t.Error("Expected non-empty message")
	}
}

func TestWrapClientError_Deleted(t *testing.T) {
	clientErr := client.ErrDeleted{UID: "abc-123"}
	toolErr := WrapClientError(clientErr)

	te, ok := toolErr.(*ToolError)
	if !ok {
		t.Fatalf("Expected *ToolError, got %T", toolErr)
	}

	if te.Code != ErrCodeDeleted {
		t.Errorf("Expected code DELETED, got %s", te.Code)
	}
}

func TestWrapClientError_VersionMismatch(t *testing.T) {
	clientErr := client.ErrVersionMismatch{
		Expected: 5,
		Actual:   7,
	}
	toolErr := WrapClientError(clientErr)

	te, ok := toolErr.(*ToolError)
	if !ok {
		t.Fatalf("Expected *ToolError, got %T", toolErr)
	}

	if te.Code != ErrCodeConflict {
		t.Errorf("Expected code CONFLICT, got %s", te.Code)
	}

	// Verify data includes version info
	if te.Data == nil {
		t.Fatal("Expected data to be present for version mismatch")
	}

	if te.Data["expected"] != 5 {
		t.Errorf("Expected data.expected = 5, got %v", te.Data["expected"])
	}

	if te.Data["actual"] != 7 {
		t.Errorf("Expected data.actual = 7, got %v", te.Data["actual"])
	}
}

func TestWrapClientError_EpochMismatch(t *testing.T) {
	clientErr := client.ErrEpochMismatch{
		ClientEpoch: 10,
		ServerEpoch: 12,
	}
	toolErr := WrapClientError(clientErr)

	te, ok := toolErr.(*ToolError)
	if !ok {
		t.Fatalf("Expected *ToolError, got %T", toolErr)
	}

	if te.Code != ErrCodeConflict {
		t.Errorf("Expected code CONFLICT, got %s", te.Code)
	}

	// Verify data includes epoch info
	if te.Data == nil {
		t.Fatal("Expected data to be present for epoch mismatch")
	}

	if te.Data["clientEpoch"] != 10 {
		t.Errorf("Expected data.clientEpoch = 10, got %v", te.Data["clientEpoch"])
	}

	if te.Data["serverEpoch"] != 12 {
		t.Errorf("Expected data.serverEpoch = 12, got %v", te.Data["serverEpoch"])
	}
}

func TestWrapClientError_RateLimited(t *testing.T) {
	clientErr := client.ErrRateLimited{
		RetryAfter: 30,
	}
	toolErr := WrapClientError(clientErr)

	te, ok := toolErr.(*ToolError)
	if !ok {
		t.Fatalf("Expected *ToolError, got %T", toolErr)
	}

	if te.Code != ErrCodeRateLimit {
		t.Errorf("Expected code RATE_LIMIT, got %s", te.Code)
	}

	// Verify data includes retry-after
	if te.Data == nil {
		t.Fatal("Expected data to be present for rate limit")
	}

	if te.Data["retryAfter"] != 30 {
		t.Errorf("Expected data.retryAfter = 30, got %v", te.Data["retryAfter"])
	}
}

func TestWrapClientError_GenericError(t *testing.T) {
	clientErr := errors.New("some random error")
	toolErr := WrapClientError(clientErr)

	te, ok := toolErr.(*ToolError)
	if !ok {
		t.Fatalf("Expected *ToolError, got %T", toolErr)
	}

	if te.Code != ErrCodeInternal {
		t.Errorf("Expected code INTERNAL_ERROR, got %s", te.Code)
	}

	if te.Message != "some random error" {
		t.Errorf("Expected message 'some random error', got '%s'", te.Message)
	}
}

func TestWrapClientError_Nil(t *testing.T) {
	toolErr := WrapClientError(nil)
	if toolErr != nil {
		t.Errorf("Expected nil for nil input, got %v", toolErr)
	}
}

func TestToolError_ToJSONRPCError(t *testing.T) {
	tests := []struct {
		name         string
		toolError    *ToolError
		expectedCode int
		hasData      bool
	}{
		{
			name:         "InvalidParams",
			toolError:    NewToolError(ErrCodeInvalidParams, "bad params", nil),
			expectedCode: -32602,
			hasData:      false,
		},
		{
			name:         "NotFound",
			toolError:    NewToolError(ErrCodeNotFound, "not found", nil),
			expectedCode: -32602,
			hasData:      false,
		},
		{
			name:         "MethodNotFound",
			toolError:    NewToolError(ErrCodeMethodNotFound, "method not found", nil),
			expectedCode: -32601,
			hasData:      false,
		},
		{
			name: "Conflict with data",
			toolError: NewToolError(ErrCodeConflict, "version mismatch", map[string]any{
				"expected": 5,
				"actual":   7,
			}),
			expectedCode: -32603,
			hasData:      true,
		},
		{
			name: "RateLimit with data",
			toolError: NewToolError(ErrCodeRateLimit, "rate limited", map[string]any{
				"retryAfter": 30,
			}),
			expectedCode: -32603,
			hasData:      true,
		},
		{
			name:         "Internal",
			toolError:    NewToolError(ErrCodeInternal, "internal error", nil),
			expectedCode: -32603,
			hasData:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, message, data := tt.toolError.ToJSONRPCError()

			if code != tt.expectedCode {
				t.Errorf("Expected code %d, got %d", tt.expectedCode, code)
			}

			if message != tt.toolError.Message {
				t.Errorf("Expected message '%s', got '%s'", tt.toolError.Message, message)
			}

			if tt.hasData {
				if data == nil {
					t.Error("Expected data to be present")
				} else {
					// Verify data is valid JSON
					var decoded map[string]any
					if err := json.Unmarshal(data, &decoded); err != nil {
						t.Errorf("Data is not valid JSON: %v", err)
					}
				}
			} else {
				if data != nil {
					t.Error("Expected data to be nil")
				}
			}
		})
	}
}

func TestToolError_Error(t *testing.T) {
	te := NewToolError(ErrCodeInvalidParams, "bad input", nil)
	errStr := te.Error()

	// Should include both code and message
	if errStr == "" {
		t.Error("Error() returned empty string")
	}

	// Basic check that it contains the code and message
	expectedSubstrings := []string{"INVALID_PARAMS", "bad input"}
	for _, substr := range expectedSubstrings {
		if !contains(errStr, substr) {
			t.Errorf("Error string should contain '%s', got '%s'", substr, errStr)
		}
	}
}

func TestNewToolError(t *testing.T) {
	data := map[string]any{
		"field": "test",
		"value": 42,
	}

	te := NewToolError(ErrCodeConflict, "conflict occurred", data)

	if te.Code != ErrCodeConflict {
		t.Errorf("Expected code CONFLICT, got %s", te.Code)
	}

	if te.Message != "conflict occurred" {
		t.Errorf("Expected message 'conflict occurred', got '%s'", te.Message)
	}

	if te.Data == nil {
		t.Fatal("Expected data to be set")
	}

	if te.Data["field"] != "test" {
		t.Errorf("Expected data.field = 'test', got %v", te.Data["field"])
	}

	if te.Data["value"] != 42 {
		t.Errorf("Expected data.value = 42, got %v", te.Data["value"])
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr || len(s) > len(substr) && contains(s[1:], substr)
}
