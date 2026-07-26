package agentruntimemcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// listToolsInput is the empty input for the list_tools introspection tool.
type listToolsInput struct{}

// listToolsEntry is one tool row returned by list_tools (Control discover merges hold flags).
type listToolsEntry struct {
	Name         string         `json:"name"`
	Hold         bool           `json:"hold"`
	Description  string         `json:"description,omitempty"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema"`
}

type listToolsOutput struct {
	Tools []listToolsEntry `json:"tools"`
}

func handleListTools(_ context.Context, _ *mcp.CallToolRequest, _ listToolsInput) (*mcp.CallToolResult, listToolsOutput, error) {
	regs := RegisteredTools()
	tools := make([]listToolsEntry, 0, len(regs))
	for _, reg := range regs {
		tools = append(tools, listToolsEntry{
			Name:         reg.Name,
			Hold:         reg.Hold,
			Description:  reg.Description,
			InputSchema:  map[string]any{},
			OutputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		})
	}
	return nil, listToolsOutput{Tools: tools}, nil
}

// RegisterIntrospectionTools adds AgentRuntime operator tools (list_tools) to an MCP server.
// Call after adapter Register; use ResetToolRegistry before Register in multi-adapter routers.
func RegisterIntrospectionTools(server *mcp.Server) {
	if server == nil {
		return
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_tools",
		Description: "AgentRuntime introspection: registered tool names and hold flags for catalog publish.",
	}, handleListTools)
}

// PrepareAdapterRegistration clears the per-adapter hold registry, runs adapter Register,
// then attaches list_tools for Control discover.
func PrepareAdapterRegistration(server *mcp.Server, schema SchemaWriter, register func(*mcp.Server, SchemaWriter)) {
	ResetToolRegistry()
	register(server, schema)
	RegisterIntrospectionTools(server)
}
