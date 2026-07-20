package agentruntimemcp

import (
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolDef is the AgentRuntime tool registration record.
// Prefer ToolDef over naked mcp.Tool so hold and catalog tooling can read intent from source.
type ToolDef struct {
	Name        string
	Description string
	// Hold marks the tool as registered in dev but excluded from catalog/publish until cleared.
	Hold bool
	// InputSchema when non-nil is copied to the MCP tool before registration.
	// When nil, AddTool infers schema from the handler input type.
	InputSchema any
}

// ToolRegistration is a registered tool entry for introspection (catalog, CI).
type ToolRegistration struct {
	Name        string
	Description string
	Hold        bool
}

// ToolOption adjusts tool registration (e.g. hold without switching from mcp.Tool literals).
type ToolOption func(*ToolDef)

// WithHold marks a tool as held back from catalog/publish.
func WithHold() ToolOption {
	return func(def *ToolDef) {
		def.Hold = true
	}
}

var toolRegistry = struct {
	mu    sync.RWMutex
	tools []ToolRegistration
}{
	tools: make([]ToolRegistration, 0, 32),
}

func recordToolRegistration(def ToolDef) {
	toolRegistry.mu.Lock()
	defer toolRegistry.mu.Unlock()
	toolRegistry.tools = append(toolRegistry.tools, ToolRegistration{
		Name:        def.Name,
		Description: def.Description,
		Hold:        def.Hold,
	})
}

// RegisteredTools returns all tools registered via AddTool in registration order.
func RegisteredTools() []ToolRegistration {
	toolRegistry.mu.RLock()
	defer toolRegistry.mu.RUnlock()
	out := make([]ToolRegistration, len(toolRegistry.tools))
	copy(out, toolRegistry.tools)
	return out
}

// HeldToolNames returns wire names registered with hold=true.
func HeldToolNames() []string {
	toolRegistry.mu.RLock()
	defer toolRegistry.mu.RUnlock()
	var out []string
	for _, t := range toolRegistry.tools {
		if t.Hold {
			out = append(out, t.Name)
		}
	}
	return out
}

// PublishableToolNames returns wire names registered with hold=false.
func PublishableToolNames() []string {
	toolRegistry.mu.RLock()
	defer toolRegistry.mu.RUnlock()
	var out []string
	for _, t := range toolRegistry.tools {
		if !t.Hold {
			out = append(out, t.Name)
		}
	}
	return out
}

// ResetToolRegistry clears registered tools. Intended for tests only.
func ResetToolRegistry() {
	toolRegistry.mu.Lock()
	defer toolRegistry.mu.Unlock()
	toolRegistry.tools = toolRegistry.tools[:0]
}

// ToolDefFromMCP copies an MCP tool definition. Hold defaults to false.
func ToolDefFromMCP(t *mcp.Tool) *ToolDef {
	if t == nil {
		return nil
	}
	return &ToolDef{
		Name:        t.Name,
		Description: t.Description,
		InputSchema: t.InputSchema,
	}
}

func toolDefFromArg(tool any, opts ...ToolOption) ToolDef {
	var def ToolDef
	switch t := tool.(type) {
	case *ToolDef:
		if t == nil {
			panic("agentruntimemcp.AddTool: nil *ToolDef")
		}
		def = *t
	case ToolDef:
		def = t
	case *mcp.Tool:
		if t == nil {
			panic("agentruntimemcp.AddTool: nil *mcp.Tool")
		}
		def = ToolDef{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	default:
		panic("agentruntimemcp.AddTool: tool must be *ToolDef, ToolDef, or *mcp.Tool")
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&def)
		}
	}
	if def.Name == "" {
		panic("agentruntimemcp.AddTool: tool name is required")
	}
	return def
}
