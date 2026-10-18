package relay_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agentruntime-io/agentruntime-mcp-go/relay"
	"github.com/gorilla/websocket"
)

func TestHandleMCPForward_syncResponse(t *testing.T) {
	h := relay.NewHub()
	mux := relay.NewRequestMultiplexer(2 * time.Second)
	conn := &relay.Conn{
		InstanceID: "inst-1",
		TenantID:   "t1",
		Mux:        mux,
		Send: func(b []byte) error {
			var frame struct {
				Type string `json:"type"`
				ID   string `json:"id"`
			}
			_ = json.Unmarshal(b, &frame)
			if frame.Type == "mcp_request" && frame.ID != "" {
				go mux.Complete(frame.ID, []byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
			}
			return nil
		},
	}
	h.RegisterMCP("inst-1", conn)

	srv := relay.NewServer(relay.ServerConfig{})
	srv.Hub = h
	m := http.NewServeMux()
	srv.RegisterRoutes(m)
	ts := httptest.NewServer(m)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/relay/mcp/instances/inst-1", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["result"] == nil {
		t.Fatalf("expected JSON-RPC result, got %#v", out)
	}
}

func TestHandleHarnessPick(t *testing.T) {
	h := relay.NewHub()
	conn := &relay.Conn{
		WorkerID: "shared:a", TenantID: "t1", Tier: "lightweight",
		Providers: []string{"codex"}, MaxConcurrent: 2,
		Send: func([]byte) error { return nil },
	}
	h.RegisterHarness("shared:a", conn)

	srv := relay.NewServer(relay.ServerConfig{})
	srv.Hub = h
	m := http.NewServeMux()
	srv.RegisterRoutes(m)
	ts := httptest.NewServer(m)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/relay/harness/pick?tenant_id=t1&provider=codex&tier=lightweight")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var out struct {
		WorkerID string `json:"worker_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.WorkerID != "shared:a" {
		t.Fatalf("expected shared:a, got %s", out.WorkerID)
	}
}

func TestMCPConnect_dispatchesResponse(t *testing.T) {
	h := relay.NewHub()
	srv := relay.NewServer(relay.ServerConfig{AuthToken: "tok"})
	srv.Hub = h
	m := http.NewServeMux()
	srv.RegisterRoutes(m)
	ts := httptest.NewServer(m)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/connect?instance_id=inst-ws&tenant_id=t1"
	header := http.Header{}
	header.Set("Authorization", "Bearer tok")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)
	conn, ok := h.GetMCP("inst-ws")
	if !ok || conn.Mux == nil {
		t.Fatal("expected MCP conn registered with mux")
	}

	ch := conn.Mux.Register("corr-1")
	_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"mcp_response","id":"corr-1","body":{"jsonrpc":"2.0","result":{}}}`))
	body, ok := conn.Mux.Wait(ch)
	if !ok || len(body) == 0 {
		t.Fatalf("expected mux completion via WSS read loop, ok=%v", ok)
	}
}
