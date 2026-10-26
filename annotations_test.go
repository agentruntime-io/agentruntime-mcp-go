package agentruntimemcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The wire is what the platform reads: ReadOnly sends readOnlyHint, Writes sends destructiveHint (a hint MCP
// defines only for tools that are not read-only), and a tool without annotations sends neither.
func TestReadOnlyAndWritesReachToolsList(t *testing.T) {
	ResetToolRegistry()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	AddTool(server, &ToolDef{Name: "acme_list_items", Description: "read", Annotations: ReadOnly()}, holdHandler)
	AddTool(server, &mcp.Tool{Name: "acme_get_updates", Description: "acknowledges", Annotations: Writes(true)}, holdHandler)
	AddTool(server, &mcp.Tool{Name: "acme_find_or_add", Description: "adds", Annotations: Writes(false)}, holdHandler)
	AddTool(server, &mcp.Tool{Name: "acme_list_users", Description: "no hint"}, holdHandler)

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	res, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]map[string]any{}
	for _, tool := range res.Tools {
		raw, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		var wire struct {
			Annotations map[string]any `json:"annotations"`
		}
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatal(err)
		}
		got[tool.Name] = wire.Annotations
	}
	if got["acme_list_items"]["readOnlyHint"] != true {
		t.Fatalf("ReadOnly: annotations = %v", got["acme_list_items"])
	}
	if got["acme_get_updates"]["destructiveHint"] != true || got["acme_get_updates"]["readOnlyHint"] == true {
		t.Fatalf("Writes(true): annotations = %v", got["acme_get_updates"])
	}
	if got["acme_find_or_add"]["destructiveHint"] != false {
		t.Fatalf("Writes(false): annotations = %v", got["acme_find_or_add"])
	}
	if got["acme_list_users"] != nil {
		t.Fatalf("no annotations: got %v", got["acme_list_users"])
	}
}

func TestWritesDoesNotShareState(t *testing.T) {
	a, b := Writes(true), Writes(false)
	if *a.DestructiveHint != true || *b.DestructiveHint != false {
		t.Fatalf("Writes(true) = %v, Writes(false) = %v", *a.DestructiveHint, *b.DestructiveHint)
	}
}
