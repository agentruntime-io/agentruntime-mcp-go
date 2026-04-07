package agentruntimemcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBuildRuntimeContext_InstanceHeader(t *testing.T) {
	const wantInst = "550e8400-e29b-41d4-a716-446655440000"
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set(HeaderMCPInstanceID, wantInst)
	req.Header.Set("X-MCP-Tool-Name", "list_buckets")

	ctx := buildRuntimeContext(req)
	if got, _ := ctx["instance_id"].(string); got != wantInst {
		t.Fatalf("instance_id: got %q, want %q", got, wantInst)
	}
	if got, _ := ctx["tool_name"].(string); got != "list_buckets" {
		t.Fatalf("tool_name: got %v", got)
	}
}

func TestBuildRuntimeContext_ServerIDEnv(t *testing.T) {
	t.Setenv("MCP_SERVER_ID", "srv_test")

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	ctx := buildRuntimeContext(req)
	if got, _ := ctx["server_id"].(string); got != "srv_test" {
		t.Fatalf("server_id: got %v", got)
	}
}
