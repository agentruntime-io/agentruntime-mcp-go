package agentruntimemcp

import (
	"fmt"

	"github.com/agentruntime-io/agentruntime-mcp-go/toolschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// AddTool registers an MCP tool with input schema inferred from the handler's In type.
// When Tool.InputSchema is nil, builds schema via toolschema.For[In], which applies
// agentschema struct tags (minLength, maxLength) in addition to json/jsonschema tags.
func AddTool[In, Out any](server *mcp.Server, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	if t.InputSchema == nil {
		schema, err := toolschema.For[In](nil)
		if err != nil {
			panic(fmt.Errorf("AddTool %q: input schema: %w", t.Name, err))
		}
		t.InputSchema = schema
	}
	mcp.AddTool(server, t, h)
}
