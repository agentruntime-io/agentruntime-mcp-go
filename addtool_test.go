package agentruntimemcp

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type holdIn struct {
	X string `json:"x"`
}

type holdOut struct {
	Y string `json:"y"`
}

func holdHandler(ctx context.Context, req *mcp.CallToolRequest, in holdIn) (*mcp.CallToolResult, holdOut, error) {
	return nil, holdOut{Y: in.X}, nil
}

func TestAddTool_holdRegistry(t *testing.T) {
	ResetToolRegistry()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)

	AddTool(server, &ToolDef{Name: "pub_a", Description: "publishable"}, holdHandler)
	AddTool(server, &ToolDef{Name: "held_b", Description: "held", Hold: true}, holdHandler)
	AddTool(server, &ToolDef{Name: "held_c", Description: "held via option"}, holdHandler, WithHold())
	AddTool(server, &mcp.Tool{Name: "held_d", Description: "legacy mcp.Tool with WithHold"}, holdHandler, WithHold())

	gotHeld := HeldToolNames()
	wantHeld := []string{"held_b", "held_c", "held_d"}
	if len(gotHeld) != len(wantHeld) {
		t.Fatalf("held: got %v want %v", gotHeld, wantHeld)
	}
	for i := range wantHeld {
		if gotHeld[i] != wantHeld[i] {
			t.Fatalf("held[%d]: got %q want %q", i, gotHeld[i], wantHeld[i])
		}
	}

	gotPub := PublishableToolNames()
	if len(gotPub) != 1 || gotPub[0] != "pub_a" {
		t.Fatalf("publishable: got %v", gotPub)
	}

	regs := RegisteredTools()
	if len(regs) != 4 {
		t.Fatalf("registered: got %d want 4", len(regs))
	}
}
