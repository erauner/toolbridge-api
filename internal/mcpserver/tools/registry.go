package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Registry manages tool definitions and dispatches tool calls
type Registry struct {
	mu       sync.RWMutex
	tools    map[string]*toolEntry
	ordering []string // Preserve registration order for consistent tools/list
}

type toolEntry struct {
	def     ToolDefinition
	handler Handler
}

// NewRegistry creates an empty tool registry
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]*toolEntry),
	}
}

// Register adds a tool definition and handler to the registry
func (r *Registry) Register(def ToolDefinition, handler Handler) error {
	if def.Name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}
	if handler == nil {
		return fmt.Errorf("handler cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[def.Name]; exists {
		return fmt.Errorf("tool %s already registered", def.Name)
	}

	r.tools[def.Name] = &toolEntry{
		def:     def,
		handler: handler,
	}
	r.ordering = append(r.ordering, def.Name)

	return nil
}

// MustRegister registers a tool or panics on error (for init-time registration)
func (r *Registry) MustRegister(def ToolDefinition, handler Handler) {
	if err := r.Register(def, handler); err != nil {
		panic(err)
	}
}

// List returns all registered tool descriptors (for tools/list response)
func (r *Registry) List() []ToolDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	descriptors := make([]ToolDescriptor, 0, len(r.ordering))
	for _, name := range r.ordering {
		entry := r.tools[name]
		descriptors = append(descriptors, ToolDescriptor{
			Name:        entry.def.Name,
			Description: entry.def.Description,
			InputSchema: entry.def.InputSchema,
		})
	}

	return descriptors
}

// Call executes a tool by name with the given parameters
// Returns a CallResult wrapped in MCP content format per the MCP specification
func (r *Registry) Call(ctx context.Context, toolCtx *ToolContext, req CallRequest) (interface{}, error) {
	r.mu.RLock()
	entry, exists := r.tools[req.Name]
	r.mu.RUnlock()

	if !exists {
		return nil, NewToolError(ErrCodeMethodNotFound, fmt.Sprintf("Tool not found: %s", req.Name), nil)
	}

	// Execute handler
	result, err := entry.handler(ctx, toolCtx, req.Arguments)
	if err != nil {
		return nil, err
	}

	// Wrap result in MCP content format
	// Per MCP spec, tool results must be in the format: {"content": [{"type": "text", "text": "..."}]}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, NewToolError(ErrCodeInternal, "Failed to serialize tool result: "+err.Error(), nil)
	}

	return CallResult{
		Content: []ContentBlock{
			{
				Type: "text",
				Text: string(resultJSON),
			},
		},
		IsError: false,
	}, nil
}

// Get retrieves a tool definition by name (for testing)
func (r *Registry) Get(name string) (*ToolDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.tools[name]
	if !exists {
		return nil, false
	}

	return &entry.def, true
}
