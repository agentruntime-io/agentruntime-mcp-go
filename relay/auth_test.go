package relay_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentruntime-io/agentruntime-mcp-go/relay"
	"github.com/gorilla/websocket"
)

const authTestToken = "relay-secret-token"

// relayDo sends one request, with the relay token as an Authorization bearer when token is not empty.
func relayDo(t *testing.T, method, url, token, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return doRequest(t, req)
}

func doRequest(t *testing.T, req *http.Request) *http.Response {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

type authFixture struct {
	ts       *httptest.Server
	hub      *relay.Hub
	mu       sync.Mutex
	mcpSeen  []map[string]any // mcp_request frames the connector received
	jobsSeen []map[string]any // harness_run frames the worker received
}

// newAuthFixture serves the relay with a live MCP instance "inst-1" and a live shared harness worker
// "shared:a" for tenant t1, so any non-200 on those routes comes from authentication and not from a missing
// connection. The worker never completes a job, so it has room for every run the tests dispatch to it.
func newAuthFixture(t *testing.T, authToken string) *authFixture {
	t.Helper()
	f := &authFixture{hub: relay.NewHub()}

	mux := relay.NewRequestMultiplexer(2 * time.Second)
	f.hub.RegisterMCP("inst-1", &relay.Conn{
		InstanceID: "inst-1",
		TenantID:   "t1",
		Mux:        mux,
		Send: func(b []byte) error {
			var frame map[string]any
			_ = json.Unmarshal(b, &frame)
			f.mu.Lock()
			f.mcpSeen = append(f.mcpSeen, frame)
			f.mu.Unlock()
			if id, _ := frame["id"].(string); id != "" {
				go mux.Complete(id, []byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
			}
			return nil
		},
	})
	f.hub.RegisterHarness("shared:a", &relay.Conn{
		WorkerID: "shared:a", TenantID: "t1", Tier: "lightweight",
		Providers: []string{"codex"}, MaxConcurrent: 100,
		Send: func(b []byte) error {
			var frame map[string]any
			_ = json.Unmarshal(b, &frame)
			f.mu.Lock()
			f.jobsSeen = append(f.jobsSeen, frame)
			f.mu.Unlock()
			return nil
		},
	})

	srv := relay.NewServer(relay.ServerConfig{AuthToken: authToken})
	srv.Hub = f.hub
	m := http.NewServeMux()
	srv.RegisterRoutes(m)
	f.ts = httptest.NewServer(m)
	t.Cleanup(f.ts.Close)
	return f
}

// relayRoutes is every route the relay mounts, each aimed at the fixture's live connections.
var relayRoutes = []struct {
	name   string
	method string
	path   string
	body   string
}{
	{"mcp connect", http.MethodGet, "/v1/connect?instance_id=inst-x&tenant_id=t1", ""},
	{"harness connect", http.MethodGet, "/v1/harness/connect?worker_id=w-x&tenant_id=t1", ""},
	{"mcp forward", http.MethodPost, "/relay/mcp/instances/inst-1", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`},
	{"harness run", http.MethodPost, "/relay/harness/workers/shared:a/run", `{"run_id":"r1","provider":"codex","prompt":"hi"}`},
	{"harness pick", http.MethodGet, "/relay/harness/pick?tenant_id=t1&provider=codex&tier=lightweight", ""},
}

const genericUnauthorizedBody = "unauthorized\n"

func TestRelayRoutes_rejectUnauthenticated(t *testing.T) {
	f := newAuthFixture(t, authTestToken)

	cases := []struct {
		name   string
		header http.Header
	}{
		{"no credentials", http.Header{}},
		{"wrong bearer", http.Header{"Authorization": {"Bearer not-the-token"}}},
		{"empty bearer", http.Header{"Authorization": {"Bearer "}}},
		{"basic scheme", http.Header{"Authorization": {"Basic " + authTestToken}}},
		{"bare token", http.Header{"Authorization": {authTestToken}}},
		{"wrong relay header", http.Header{relay.RelayTokenHeader: {"not-the-token"}}},
		{"wrong relay header beats right bearer", http.Header{
			relay.RelayTokenHeader: {"not-the-token"},
			"Authorization":        {"Bearer " + authTestToken},
		}},
		{"prefix of the token", http.Header{"Authorization": {"Bearer " + authTestToken[:len(authTestToken)-1]}}},
		{"token with suffix", http.Header{"Authorization": {"Bearer " + authTestToken + "x"}}},
	}
	for _, route := range relayRoutes {
		for _, tc := range cases {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				req, err := http.NewRequest(route.method, f.ts.URL+route.path, strings.NewReader(route.body))
				if err != nil {
					t.Fatal(err)
				}
				for k, v := range tc.header {
					req.Header[k] = v
				}
				resp := doRequest(t, req)
				if resp.StatusCode != http.StatusUnauthorized {
					t.Fatalf("expected 401, got %d", resp.StatusCode)
				}
				body, _ := io.ReadAll(resp.Body)
				if string(body) != genericUnauthorizedBody {
					t.Fatalf("401 body must be generic, got %q", body)
				}
			})
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.mcpSeen) != 0 || len(f.jobsSeen) != 0 {
		t.Fatalf("rejected requests reached a connection: mcp=%d jobs=%d", len(f.mcpSeen), len(f.jobsSeen))
	}
}

func TestRelayRoutes_failClosedWithoutConfiguredToken(t *testing.T) {
	// A blank or whitespace-only token is "not configured". It must not be matchable by sending nothing, a
	// blank value, or any bearer at all (the old behaviour accepted any non-empty bearer).
	for _, configured := range []string{"", "   "} {
		f := newAuthFixture(t, configured)
		headers := []http.Header{
			{},
			{"Authorization": {"Bearer anything"}},
			{"Authorization": {"Bearer " + authTestToken}},
			{"Authorization": {"Bearer "}},
			{relay.RelayTokenHeader: {"anything"}},
			{relay.RelayTokenHeader: {configured}},
		}
		for _, route := range relayRoutes {
			for i, h := range headers {
				req, err := http.NewRequest(route.method, f.ts.URL+route.path, strings.NewReader(route.body))
				if err != nil {
					t.Fatal(err)
				}
				for k, v := range h {
					req.Header[k] = v
				}
				resp := doRequest(t, req)
				if resp.StatusCode != http.StatusUnauthorized {
					t.Fatalf("token %q, %s, header set %d: expected 401, got %d", configured, route.name, i, resp.StatusCode)
				}
				body, _ := io.ReadAll(resp.Body)
				if string(body) != genericUnauthorizedBody {
					t.Fatalf("401 body must be generic, got %q", body)
				}
			}
		}
		f.mu.Lock()
		reached := len(f.mcpSeen) + len(f.jobsSeen)
		f.mu.Unlock()
		if reached != 0 {
			t.Fatalf("token %q: %d requests reached a connection", configured, reached)
		}
	}
}

func TestRelayRoutes_proceedWithCorrectToken(t *testing.T) {
	f := newAuthFixture(t, authTestToken)

	cases := []struct {
		name   string
		header http.Header
	}{
		{"authorization bearer", http.Header{"Authorization": {"Bearer " + authTestToken}}},
		{"authorization bearer, lowercase scheme", http.Header{"Authorization": {"bearer " + authTestToken}}},
		{"relay token header", http.Header{relay.RelayTokenHeader: {authTestToken}}},
	}
	routes := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"mcp forward", http.MethodPost, "/relay/mcp/instances/inst-1", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, http.StatusOK},
		{"harness run", http.MethodPost, "/relay/harness/workers/shared:a/run", `{"run_id":"r1","provider":"codex","prompt":"hi"}`, http.StatusAccepted},
		{"harness pick", http.MethodGet, "/relay/harness/pick?tenant_id=t1&provider=codex&tier=lightweight", "", http.StatusOK},
		// Authenticated, so the handler itself answers: a plain GET is not a websocket upgrade.
		{"mcp connect", http.MethodGet, "/v1/connect?instance_id=inst-x&tenant_id=t1", "", http.StatusBadRequest},
		{"harness connect", http.MethodGet, "/v1/harness/connect?worker_id=w-x&tenant_id=t1", "", http.StatusBadRequest},
	}
	for _, route := range routes {
		for _, tc := range cases {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				req, err := http.NewRequest(route.method, f.ts.URL+route.path, strings.NewReader(route.body))
				if err != nil {
					t.Fatal(err)
				}
				for k, v := range tc.header {
					req.Header[k] = v
				}
				resp := doRequest(t, req)
				if resp.StatusCode != route.want {
					b, _ := io.ReadAll(resp.Body)
					t.Fatalf("expected %d, got %d %s", route.want, resp.StatusCode, b)
				}
			})
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.mcpSeen) != len(cases) {
		t.Fatalf("expected %d forwarded MCP frames, got %d", len(cases), len(f.mcpSeen))
	}
	if len(f.jobsSeen) != len(cases) {
		t.Fatalf("expected %d dispatched jobs, got %d", len(cases), len(f.jobsSeen))
	}
}

func TestRelayConnect_websocketNeedsToken(t *testing.T) {
	f := newAuthFixture(t, authTestToken)
	for _, path := range []string{"/v1/connect?instance_id=inst-ws&tenant_id=t1", "/v1/harness/connect?worker_id=w-ws&tenant_id=t1"} {
		wsURL := "ws" + strings.TrimPrefix(f.ts.URL, "http") + path

		_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err == nil {
			t.Fatalf("%s: dial without a token must fail", path)
		}
		if resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: expected 401 on the upgrade, got %v", path, resp)
		}

		header := http.Header{}
		header.Set("Authorization", "Bearer "+authTestToken)
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, header)
		if err != nil {
			t.Fatalf("%s: dial with the token failed: %v", path, err)
		}
		ws.Close()
	}
}

func TestHandleMCPForward_neverHandsRelayTokenToConnector(t *testing.T) {
	const connectorBearer = "Bearer downstream-connector-credential"
	cases := []struct {
		name     string
		header   http.Header
		wantAuth string
	}{
		{
			name:     "token in Authorization is withheld",
			header:   http.Header{"Authorization": {"Bearer " + authTestToken}},
			wantAuth: "",
		},
		{
			name:     "X-Relay-Token leaves Authorization for the connector",
			header:   http.Header{relay.RelayTokenHeader: {authTestToken}, "Authorization": {connectorBearer}},
			wantAuth: connectorBearer,
		},
		{
			name:     "token in both headers is still withheld",
			header:   http.Header{relay.RelayTokenHeader: {authTestToken}, "Authorization": {"Bearer " + authTestToken}},
			wantAuth: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAuthFixture(t, authTestToken)
			req, err := http.NewRequest(http.MethodPost, f.ts.URL+"/relay/mcp/instances/inst-1", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			if err != nil {
				t.Fatal(err)
			}
			for k, v := range tc.header {
				req.Header[k] = v
			}
			if resp := doRequest(t, req); resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}

			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.mcpSeen) != 1 {
				t.Fatalf("expected one frame, got %d", len(f.mcpSeen))
			}
			raw, _ := json.Marshal(f.mcpSeen[0])
			if strings.Contains(string(raw), authTestToken) {
				t.Fatalf("relay token reached the connector: %s", raw)
			}
			headers, _ := f.mcpSeen[0]["headers"].(map[string]any)
			if got, _ := headers["authorization"].(string); got != tc.wantAuth {
				t.Fatalf("connector authorization = %q, want %q", got, tc.wantAuth)
			}
		})
	}
}

func TestHandleHarnessPick_requiresTenantID(t *testing.T) {
	f := newAuthFixture(t, authTestToken)
	// A second tenant's worker: an empty tenant_id used to be handed whichever worker matched first.
	f.hub.RegisterHarness("shared:b", &relay.Conn{
		WorkerID: "shared:b", TenantID: "t2", Tier: "lightweight",
		Providers: []string{"codex"}, MaxConcurrent: 2,
		Send: func([]byte) error { return nil },
	})

	cases := []struct {
		name  string
		query string
		want  int
	}{
		{"tenant_id absent", "provider=codex&tier=lightweight", http.StatusBadRequest},
		{"tenant_id empty", "tenant_id=&provider=codex&tier=lightweight", http.StatusBadRequest},
		{"tenant_id blank", "tenant_id=%20%20&provider=codex&tier=lightweight", http.StatusBadRequest},
		{"no query at all", "", http.StatusBadRequest},
		{"tenant_id present", "tenant_id=t2&provider=codex&tier=lightweight", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := relayDo(t, http.MethodGet, f.ts.URL+"/relay/harness/pick?"+tc.query, authTestToken, "")
			if resp.StatusCode != tc.want {
				t.Fatalf("expected %d, got %d", tc.want, resp.StatusCode)
			}
			if tc.want == http.StatusOK {
				var out struct {
					WorkerID string `json:"worker_id"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
					t.Fatal(err)
				}
				if out.WorkerID != "shared:b" {
					t.Fatalf("tenant t2 must be handed its own worker, got %q", out.WorkerID)
				}
			}
		})
	}
}
