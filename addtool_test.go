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

func TestAddTool_annotationsReachToolsList(t *testing.T) {
	ResetToolRegistry()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)

	AddTool(server, &mcp.Tool{
		Name:        "legacy_read",
		Description: "legacy mcp.Tool marked read-only",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, holdHandler)
	AddTool(server, &ToolDef{
		Name:        "def_read",
		Description: "ToolDef marked read-only",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, holdHandler)
	AddTool(server, &mcp.Tool{Name: "plain_write", Description: "no annotations"}, holdHandler)

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	res, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = tool.Annotations != nil && tool.Annotations.ReadOnlyHint
	}
	want := map[string]bool{"legacy_read": true, "def_read": true, "plain_write": false}
	for name, readOnly := range want {
		if v, ok := got[name]; !ok || v != readOnly {
			t.Fatalf("%s: readOnlyHint got %v (listed %v) want %v", name, v, ok, readOnly)
		}
	}
}
