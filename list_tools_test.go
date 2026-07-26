package agentruntimemcp

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestListTools_holdFlags(t *testing.T) {
	ResetToolRegistry()
	t.Cleanup(ResetToolRegistry)

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	AddTool(server, &ToolDef{Name: "pub_a", Description: "publishable"}, holdHandler)
	AddTool(server, &ToolDef{Name: "held_b", Description: "held", Hold: true}, holdHandler)
	RegisterIntrospectionTools(server)

	_, out, err := handleListTools(context.Background(), nil, listToolsInput{})
	if err != nil {
		t.Fatalf("handleListTools: %v", err)
	}
	holdByName := map[string]bool{}
	for _, tool := range out.Tools {
		if tool.Name == "list_tools" {
			t.Fatalf("list_tools introspection should not appear in output")
		}
		holdByName[tool.Name] = tool.Hold
	}
	if holdByName["pub_a"] {
		t.Fatal("pub_a should not be held")
	}
	if !holdByName["held_b"] {
		t.Fatal("held_b should be held")
	}
}
