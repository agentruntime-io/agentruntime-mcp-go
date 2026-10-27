package relay_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

	srv := relay.NewServer(relay.ServerConfig{InternalToken: "test-internal"})
	srv.Hub = h
	m := http.NewServeMux()
	srv.RegisterRoutes(m)
	ts := httptest.NewServer(m)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/relay/harness/pick?tenant_id=t1&provider=codex&tier=lightweight", nil)
	req.Header.Set("X-Internal-Token", "test-internal")
	resp, err := http.DefaultClient.Do(req)
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

// ---- authentication ----------------------------------------------------------

// fakePlatform stands in for the BFF (/v1/me) and Wheelhouse (/internal/pats/introspect).
// All tokens are made up.
type fakePlatform struct {
	down bool // answer 500 to everything
}

const (
	testWheelhouseToken = "test-wheelhouse-internal"
	testJWTAlice        = "test.jwt.alice" // user-alice, member of tenant-a only
	testPATAlice        = "pat_test_alice" // tenant-a, user-alice
	testPATService      = "pat_test_svc"   // tenant-a, no user
	testPATExpired      = "pat_test_expired"
)

func (f *fakePlatform) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/me", func(w http.ResponseWriter, r *http.Request) {
		if f.down {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+testJWTAlice {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if tenant := r.Header.Get("X-Tenant-Id"); tenant != "tenant-a" {
			http.Error(w, "not a member", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"tenant_id": "tenant-a", "user_id": "user-alice"})
	})
	mux.HandleFunc("/internal/pats/introspect", func(w http.ResponseWriter, r *http.Request) {
		if f.down {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		if r.Header.Get("X-Internal-Token") != testWheelhouseToken {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var body struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch body.Token {
		case testPATAlice:
			_ = json.NewEncoder(w).Encode(map[string]any{"tenant_id": "tenant-a", "user_id": "user-alice", "pat_id": "p1"})
		case testPATService:
			_ = json.NewEncoder(w).Encode(map[string]any{"tenant_id": "tenant-a", "pat_id": "p2"})
		case testPATExpired:
			_ = json.NewEncoder(w).Encode(map[string]any{"tenant_id": "tenant-a", "user_id": "user-alice", "expires_at": time.Now().Add(-time.Hour)})
		default:
			http.Error(w, "unknown", http.StatusUnauthorized)
		}
	})
	return httptest.NewServer(mux)
}

func newAuthRelay(t *testing.T, platform *httptest.Server, cfg relay.ServerConfig) (*relay.Hub, *httptest.Server) {
	t.Helper()
	if platform != nil {
		cfg.Verifier = relay.NewVerifier(relay.VerifierConfig{
			BFFURL: platform.URL, WheelhouseURL: platform.URL, WheelhouseToken: testWheelhouseToken,
		})
	}
	h := relay.NewHub()
	srv := relay.NewServer(cfg)
	srv.Hub = h
	m := http.NewServeMux()
	srv.RegisterRoutes(m)
	ts := httptest.NewServer(m)
	t.Cleanup(ts.Close)
	return h, ts
}

// dialHarness connects a worker; on refusal it returns the HTTP status instead of a socket.
func dialHarness(t *testing.T, ts *httptest.Server, bearer, query string) (*websocket.Conn, int) {
	t.Helper()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/harness/connect?" + query
	header := http.Header{}
	if bearer != "" {
		header.Set("Authorization", "Bearer "+bearer)
	}
	ws, resp, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		if resp == nil {
			t.Fatalf("dial failed without a response: %v", err)
		}
		return nil, resp.StatusCode
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws, 0
}

func waitHarness(t *testing.T, h *relay.Hub, tenantID, workerID string) *relay.Conn {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c, ok := h.GetHarness(tenantID, workerID); ok {
			return c
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("worker %q (tenant %q) never registered", workerID, tenantID)
	return nil
}

func TestHarnessConnect_refusals(t *testing.T) {
	platform := (&fakePlatform{}).server(t)
	defer platform.Close()
	_, ts := newAuthRelay(t, platform, relay.ServerConfig{AuthToken: "test-static"})

	cases := []struct {
		name   string
		bearer string
		query  string
		want   int
	}{
		{"missing bearer", "", "tenant_id=tenant-a&worker_id=workbench:user-alice", http.StatusUnauthorized},
		{"garbage bearer", "anything-at-all", "tenant_id=tenant-a&worker_id=workbench:user-alice", http.StatusUnauthorized},
		{"unknown PAT", "pat_test_nobody", "tenant_id=tenant-a", http.StatusUnauthorized},
		{"expired PAT", testPATExpired, "tenant_id=tenant-a", http.StatusUnauthorized},
		{"JWT for a tenant the user is not in", testJWTAlice, "tenant_id=tenant-b&worker_id=workbench:user-alice", http.StatusUnauthorized},
		{"JWT without a tenant", testJWTAlice, "worker_id=workbench:user-alice", http.StatusUnauthorized},
		{"PAT bound to another tenant than claimed", testPATAlice, "tenant_id=tenant-b", http.StatusForbidden},
		{"user_id of someone else", testPATAlice, "tenant_id=tenant-a&user_id=user-bob", http.StatusForbidden},
		{"another user's workbench worker id", testJWTAlice, "tenant_id=tenant-a&worker_id=workbench:user-bob", http.StatusForbidden},
		{"service PAT cannot take a workbench worker id", testPATService, "worker_id=workbench:user-alice", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, got := dialHarness(t, ts, tc.bearer, tc.query); got != tc.want {
				t.Fatalf("status = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestHarnessConnect_acceptsAndBindsIdentity(t *testing.T) {
	platform := (&fakePlatform{}).server(t)
	defer platform.Close()
	h, ts := newAuthRelay(t, platform, relay.ServerConfig{AuthToken: "test-static"})

	t.Run("valid JWT", func(t *testing.T) {
		if _, got := dialHarness(t, ts, testJWTAlice, "tenant_id=tenant-a&user_id=user-alice&worker_id=workbench:user-alice&tier=heavy"); got != 0 {
			t.Fatalf("status = %d", got)
		}
		c := waitHarness(t, h, "tenant-a", "workbench:user-alice")
		if c.TenantID != "tenant-a" || c.UserID != "user-alice" {
			t.Fatalf("conn bound to %q/%q", c.TenantID, c.UserID)
		}
	})
	t.Run("valid PAT takes tenant and user from the token", func(t *testing.T) {
		if _, got := dialHarness(t, ts, testPATAlice, "worker_id=pat-worker-1"); got != 0 {
			t.Fatalf("status = %d", got)
		}
		c := waitHarness(t, h, "tenant-a", "pat-worker-1")
		if c.TenantID != "tenant-a" || c.UserID != "user-alice" {
			t.Fatalf("conn bound to %q/%q", c.TenantID, c.UserID)
		}
	})
	t.Run("service PAT without a user", func(t *testing.T) {
		if _, got := dialHarness(t, ts, testPATService, "worker_id=svc-worker-1"); got != 0 {
			t.Fatalf("status = %d", got)
		}
		c := waitHarness(t, h, "tenant-a", "svc-worker-1")
		if c.UserID != "" {
			t.Fatalf("expected no user, got %q", c.UserID)
		}
	})
	t.Run("static dev token still works and keeps the claimed tenant", func(t *testing.T) {
		if _, got := dialHarness(t, ts, "test-static", "tenant_id=tenant-dev&user_id=u-dev&worker_id=workbench:u-dev"); got != 0 {
			t.Fatalf("status = %d", got)
		}
		c := waitHarness(t, h, "tenant-dev", "workbench:u-dev")
		if c.UserID != "u-dev" {
			t.Fatalf("user = %q", c.UserID)
		}
	})
}

func TestHarnessConnect_verifierDownIs503(t *testing.T) {
	platform := (&fakePlatform{down: true}).server(t)
	defer platform.Close()
	_, ts := newAuthRelay(t, platform, relay.ServerConfig{})
	if _, got := dialHarness(t, ts, testPATAlice, "tenant_id=tenant-a"); got != http.StatusServiceUnavailable {
		t.Fatalf("PAT while platform is down: status = %d, want 503", got)
	}
	if _, got := dialHarness(t, ts, testJWTAlice, "tenant_id=tenant-a"); got != http.StatusServiceUnavailable {
		t.Fatalf("JWT while platform is down: status = %d, want 503", got)
	}
}

func TestHarnessConnect_failsClosedWithoutConfig(t *testing.T) {
	_, ts := newAuthRelay(t, nil, relay.ServerConfig{})
	if _, got := dialHarness(t, ts, "any-bearer", "tenant_id=t1"); got != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured relay: status = %d, want 503", got)
	}
}

func TestHarnessConnect_insecureFlagOpensIt(t *testing.T) {
	h, ts := newAuthRelay(t, nil, relay.ServerConfig{AllowInsecure: true})
	if _, got := dialHarness(t, ts, "any-bearer", "tenant_id=t1&worker_id=w1"); got != 0 {
		t.Fatalf("status = %d", got)
	}
	waitHarness(t, h, "t1", "w1")
}

func TestHarnessConnect_staticOnlyRejectsOtherBearers(t *testing.T) {
	_, ts := newAuthRelay(t, nil, relay.ServerConfig{AuthToken: "test-static"})
	if _, got := dialHarness(t, ts, "not-the-static-token", "tenant_id=t1"); got != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", got)
	}
}

func TestMCPConnect_usesVerifiedTenant(t *testing.T) {
	platform := (&fakePlatform{}).server(t)
	defer platform.Close()
	h, ts := newAuthRelay(t, platform, relay.ServerConfig{})
	base := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/connect?instance_id=inst-1"

	// a tenant in the query that the token does not have
	header := http.Header{"Authorization": {"Bearer " + testPATAlice}}
	if _, resp, err := websocket.DefaultDialer.Dial(base+"&tenant_id=tenant-b", header); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("mismatched tenant: err=%v resp=%v, want 403", err, resp)
	}
	// no tenant in the query: the token's tenant is used
	ws, _, err := websocket.DefaultDialer.Dial(base, header)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c, ok := h.GetMCP("inst-1"); ok {
			if c.TenantID != "tenant-a" {
				t.Fatalf("instance bound to tenant %q", c.TenantID)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("instance never registered")
}

func TestMCPConnect_refusesAnotherTenantsLiveInstance(t *testing.T) {
	h := relay.NewHub()
	h.RegisterMCP("inst-9", &relay.Conn{TenantID: "tenant-b", Send: func([]byte) error { return nil }})
	srv := relay.NewServer(relay.ServerConfig{AuthToken: "tok"})
	srv.Hub = h
	m := http.NewServeMux()
	srv.RegisterRoutes(m)
	ts := httptest.NewServer(m)
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/connect?instance_id=inst-9&tenant_id=tenant-a"
	_, resp, err := websocket.DefaultDialer.Dial(url, http.Header{"Authorization": {"Bearer tok"}})
	if err == nil || resp == nil || resp.StatusCode != http.StatusConflict {
		t.Fatalf("err=%v resp=%v, want 409", err, resp)
	}
	if c, _ := h.GetMCP("inst-9"); c == nil || c.TenantID != "tenant-b" {
		t.Fatal("the other tenant's connection was displaced")
	}
}

// ---- dispatch ---------------------------------------------------------------

func dispatchRequest(t *testing.T, ts *httptest.Server, workerID string, headers map[string]string, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/relay/harness/workers/"+workerID+"/run", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// sink records the frames a registered worker is sent.
type sink struct {
	mu     sync.Mutex
	frames []string
}

func (s *sink) send(b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frames = append(s.frames, string(b))
	return nil
}

func (s *sink) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.frames...)
}

func registerWorker(h *relay.Hub, tenantID, workerID string, s *sink) *relay.Conn {
	c := &relay.Conn{TenantID: tenantID, UserID: "u", Tier: "heavy", Send: s.send}
	h.RegisterHarness(workerID, c)
	return c
}

func TestHarnessDispatch_requiresInternalTokenAndTenant(t *testing.T) {
	h, ts := newAuthRelay(t, nil, relay.ServerConfig{InternalToken: "test-internal"})
	var s sink
	registerWorker(h, "tenant-a", "workbench:user-alice", &s)
	job := `{"run_id":"r1:s1","provider":"codex","prompt":"hi"}`

	cases := []struct {
		name    string
		worker  string
		headers map[string]string
		want    int
	}{
		{"no token", "workbench:user-alice", map[string]string{"X-Tenant-Id": "tenant-a"}, http.StatusUnauthorized},
		{"wrong token", "workbench:user-alice", map[string]string{"X-Internal-Token": "nope", "X-Tenant-Id": "tenant-a"}, http.StatusUnauthorized},
		{"bearer is not the internal token", "workbench:user-alice", map[string]string{"Authorization": "Bearer test-internal", "X-Tenant-Id": "tenant-a"}, http.StatusUnauthorized},
		{"token but no tenant", "workbench:user-alice", map[string]string{"X-Internal-Token": "test-internal"}, http.StatusBadRequest},
		{"worker belongs to another tenant", "workbench:user-alice", map[string]string{"X-Internal-Token": "test-internal", "X-Tenant-Id": "tenant-b"}, http.StatusServiceUnavailable},
		{"unknown worker", "workbench:nobody", map[string]string{"X-Internal-Token": "test-internal", "X-Tenant-Id": "tenant-a"}, http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dispatchRequest(t, ts, tc.worker, tc.headers, job).StatusCode; got != tc.want {
				t.Fatalf("status = %d, want %d", got, tc.want)
			}
		})
	}
	if n := len(s.all()); n != 0 {
		t.Fatalf("refused dispatches reached the worker: %d frames", n)
	}

	good := map[string]string{"X-Internal-Token": "test-internal", "X-Tenant-Id": "tenant-a"}
	if got := dispatchRequest(t, ts, "workbench:user-alice", good, job).StatusCode; got != http.StatusAccepted {
		t.Fatalf("valid dispatch: status = %d, want 202", got)
	}
	if n := len(s.all()); n != 1 {
		t.Fatalf("worker received %d frames, want 1", n)
	}
}

func TestHarnessDispatch_noInternalTokenConfiguredFailsClosed(t *testing.T) {
	h, ts := newAuthRelay(t, nil, relay.ServerConfig{})
	var s sink
	registerWorker(h, "tenant-a", "w1", &s)
	for _, headers := range []map[string]string{
		{"X-Tenant-Id": "tenant-a"},
		{"X-Tenant-Id": "tenant-a", "X-Internal-Token": "guess"},
	} {
		if got := dispatchRequest(t, ts, "w1", headers, `{"run_id":"r:s"}`).StatusCode; got != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", got)
		}
	}
	if n := len(s.all()); n != 0 {
		t.Fatalf("%d frames reached the worker", n)
	}
}

func TestHarnessDispatch_legacyWindowAcceptsOldShape(t *testing.T) {
	h, ts := newAuthRelay(t, nil, relay.ServerConfig{InternalToken: "test-internal", AllowLegacyDispatch: true})
	var s sink
	registerWorker(h, "tenant-a", "workbench:user-alice", &s)
	job := `{"run_id":"r:s"}`

	// the old shape: no token, no tenant
	if got := dispatchRequest(t, ts, "workbench:user-alice", nil, job).StatusCode; got != http.StatusAccepted {
		t.Fatalf("legacy dispatch: status = %d, want 202", got)
	}
	// a wrong token is still refused during the window
	if got := dispatchRequest(t, ts, "workbench:user-alice", map[string]string{"X-Internal-Token": "nope"}, job).StatusCode; got != http.StatusUnauthorized {
		t.Fatalf("wrong token during window: status = %d, want 401", got)
	}
	// the new shape works too
	good := map[string]string{"X-Internal-Token": "test-internal", "X-Tenant-Id": "tenant-a"}
	if got := dispatchRequest(t, ts, "workbench:user-alice", good, job).StatusCode; got != http.StatusAccepted {
		t.Fatalf("new shape during window: status = %d, want 202", got)
	}
}

func TestHarnessDispatch_sameWorkerIDInTwoTenants(t *testing.T) {
	h, ts := newAuthRelay(t, nil, relay.ServerConfig{InternalToken: "test-internal"})
	var sentA, sentB sink
	registerWorker(h, "tenant-a", "workbench:same", &sentA)
	registerWorker(h, "tenant-b", "workbench:same", &sentB)

	for _, tenant := range []string{"tenant-a", "tenant-b", "tenant-b"} {
		headers := map[string]string{"X-Internal-Token": "test-internal", "X-Tenant-Id": tenant}
		if got := dispatchRequest(t, ts, "workbench:same", headers, `{"run_id":"`+tenant+`:s"}`).StatusCode; got != http.StatusAccepted {
			t.Fatalf("dispatch for %s: status = %d", tenant, got)
		}
	}
	a, b := sentA.all(), sentB.all()
	if len(a) != 1 || len(b) != 2 {
		t.Fatalf("tenant-a got %d jobs, tenant-b got %d; want 1 and 2", len(a), len(b))
	}
	if !strings.Contains(a[0], "tenant-a:s") {
		t.Fatalf("tenant-a worker received %s", a[0])
	}
}

func TestHarnessPick_requiresInternalTokenAndTenant(t *testing.T) {
	h, ts := newAuthRelay(t, nil, relay.ServerConfig{InternalToken: "test-internal"})
	h.RegisterHarness("shared:a", &relay.Conn{TenantID: "t1", Tier: "lightweight", Providers: []string{"codex"}, MaxConcurrent: 1, Send: func([]byte) error { return nil }})

	get := func(query string, headers map[string]string) int {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/relay/harness/pick?"+query, nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := get("tenant_id=t1&provider=codex", nil); got != http.StatusUnauthorized {
		t.Fatalf("no token: %d", got)
	}
	if got := get("tenant_id=t1&provider=codex", map[string]string{"X-Internal-Token": "nope"}); got != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", got)
	}
	if got := get("provider=codex", map[string]string{"X-Internal-Token": "test-internal"}); got != http.StatusBadRequest {
		t.Fatalf("no tenant: %d", got)
	}
	if got := get("tenant_id=t1&provider=codex", map[string]string{"X-Internal-Token": "test-internal"}); got != http.StatusOK {
		t.Fatalf("valid: %d", got)
	}
}

// ---- harness_complete -------------------------------------------------------

type recordedComplete struct {
	token string
	body  map[string]any
}

func fakeOrchestratorServer(t *testing.T) (*httptest.Server, func() []recordedComplete) {
	t.Helper()
	var mu sync.Mutex
	var got []recordedComplete
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/internal/harness/run-complete", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		got = append(got, recordedComplete{token: r.Header.Get("X-Internal-Token"), body: body})
		mu.Unlock()
	})
	mux.HandleFunc("/api/v1/internal/harness/worker-online", func(w http.ResponseWriter, r *http.Request) {})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, func() []recordedComplete {
		time.Sleep(150 * time.Millisecond) // callbacks are sent from goroutines
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedComplete(nil), got...)
	}
}

func TestHarnessComplete_onlyForDispatchedRuns(t *testing.T) {
	orch, completions := fakeOrchestratorServer(t)
	h, ts := newAuthRelay(t, nil, relay.ServerConfig{
		AuthToken:            "test-static",
		InternalToken:        "test-internal",
		OrchestratorCallback: relay.NewOrchestratorCallback(orch.URL, "test-internal"),
	})

	wsA, _ := dialHarness(t, ts, "test-static", "tenant_id=tenant-a&user_id=ua&worker_id=workbench:ua&tier=heavy")
	waitHarness(t, h, "tenant-a", "workbench:ua")
	wsB, _ := dialHarness(t, ts, "test-static", "tenant_id=tenant-b&user_id=ub&worker_id=workbench:ub&tier=heavy")
	waitHarness(t, h, "tenant-b", "workbench:ub")

	complete := func(ws *websocket.Conn, runID string) {
		_ = ws.WriteJSON(map[string]any{"type": "harness_complete", "run_id": runID, "status": "finished", "result": map[string]any{"final_text": "ok"}})
	}

	// nothing dispatched yet: both of these are forged
	complete(wsA, "run-of-nobody:step")
	complete(wsA, "")
	if n := len(completions()); n != 0 {
		t.Fatalf("%d completions for runs never dispatched", n)
	}

	// dispatch a run to tenant-a's worker
	headers := map[string]string{"X-Internal-Token": "test-internal", "X-Tenant-Id": "tenant-a"}
	if got := dispatchRequest(t, ts, "workbench:ua", headers, `{"run_id":"run-a:step-1"}`).StatusCode; got != http.StatusAccepted {
		t.Fatalf("dispatch: %d", got)
	}
	// tenant-b's worker cannot complete tenant-a's run
	complete(wsB, "run-a:step-1")
	if n := len(completions()); n != 0 {
		t.Fatalf("another tenant's worker completed the run (%d callbacks)", n)
	}
	// the worker it was dispatched to can, once, and the callback names tenant and worker
	complete(wsA, "run-a:step-1")
	got := completions()
	if len(got) != 1 {
		t.Fatalf("expected 1 callback, got %d", len(got))
	}
	if got[0].token != "test-internal" || got[0].body["run_id"] != "run-a:step-1" ||
		got[0].body["tenant_id"] != "tenant-a" || got[0].body["worker_id"] != "workbench:ua" {
		t.Fatalf("callback = %+v", got[0])
	}
	complete(wsA, "run-a:step-1") // replay
	if n := len(completions()); n != 1 {
		t.Fatalf("a completed run was accepted again (%d callbacks)", n)
	}
	if c, _ := h.GetHarness("tenant-a", "workbench:ua"); c.ActiveJobs != 0 {
		t.Fatalf("ActiveJobs = %d after the run completed", c.ActiveJobs)
	}
}

func TestHarnessComplete_survivesReconnectOfSameWorker(t *testing.T) {
	orch, completions := fakeOrchestratorServer(t)
	h, ts := newAuthRelay(t, nil, relay.ServerConfig{
		AuthToken: "test-static", InternalToken: "test-internal",
		OrchestratorCallback: relay.NewOrchestratorCallback(orch.URL, "test-internal"),
	})
	ws1, _ := dialHarness(t, ts, "test-static", "tenant_id=tenant-a&user_id=ua&worker_id=workbench:ua&tier=heavy")
	first := waitHarness(t, h, "tenant-a", "workbench:ua")
	headers := map[string]string{"X-Internal-Token": "test-internal", "X-Tenant-Id": "tenant-a"}
	if got := dispatchRequest(t, ts, "workbench:ua", headers, `{"run_id":"run-r:step"}`).StatusCode; got != http.StatusAccepted {
		t.Fatalf("dispatch: %d", got)
	}

	// the socket drops mid-job and the daemon reconnects, then reports the result
	_ = ws1.Close()
	ws2, _ := dialHarness(t, ts, "test-static", "tenant_id=tenant-a&user_id=ua&worker_id=workbench:ua&tier=heavy")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c, ok := h.GetHarness("tenant-a", "workbench:ua"); ok && c != first {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = ws2.WriteJSON(map[string]any{"type": "harness_complete", "run_id": "run-r:step", "status": "finished"})
	if n := len(completions()); n != 1 {
		t.Fatalf("result after reconnect was lost (%d callbacks)", n)
	}
}
