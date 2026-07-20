package agentruntimemcp

import (
	"fmt"

	"github.com/agentruntime-io/agentruntime-mcp-go/toolschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// AddTool registers an MCP tool with input schema inferred from the handler's In type.
// Pass *ToolDef (preferred; supports Hold) or *mcp.Tool (legacy; use WithHold() for hold).
// When InputSchema is nil, builds schema via toolschema.For[In].
//
// Held tools are still registered on the MCP server (dev tools/list); catalog generators
// should omit tools where hold is true.
func AddTool[In, Out any](server *mcp.Server, tool any, h mcp.ToolHandlerFor[In, Out], opts ...ToolOption) {
	def := toolDefFromArg(tool, opts...)
	recordToolRegistration(def)

	t := &mcp.Tool{
		Name:        def.Name,
		Description: def.Description,
		InputSchema: def.InputSchema,
	}
	if t.InputSchema == nil {
		schema, err := toolschema.For[In](nil)
		if err != nil {
			panic(fmt.Errorf("AddTool %q: input schema: %w", t.Name, err))
		}
		t.InputSchema = schema
	}
	mcp.AddTool(server, t, h)
}
