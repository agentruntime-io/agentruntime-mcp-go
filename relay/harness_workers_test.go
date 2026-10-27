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

// Tests for the worker registry: the agents a worker advertises in
// harness_register, and the internal list endpoint that reports them.

type workersReply struct {
	Workers []struct {
		WorkerID    string   `json:"worker_id"`
		TenantID    string   `json:"tenant_id"`
		UserID      string   `json:"user_id"`
		Tier        string   `json:"tier"`
		Providers   []string `json:"providers"`
		Online      bool     `json:"online"`
		ConnectedAt string   `json:"connected_at"`
		LastSeen    string   `json:"last_seen"`
		Agents      []struct {
			ID             string `json:"id"`
			AgentID        string `json:"agent_id"`
			Kind           string `json:"kind"`
			Label          string `json:"label"`
			Provider       string `json:"provider"`
			Model          string `json:"model"`
			Access         string `json:"access"`
			Network        *bool  `json:"network"`
			Web            *bool  `json:"web"`
			ConnectionName string `json:"connection_name"`
			Description    string `json:"description"`
		} `json:"agents"`
	} `json:"workers"`
}

var internalAuth = map[string]string{"X-Internal-Token": "test-internal"}

// listWorkers calls the internal list endpoint with the given headers.
func listWorkers(t *testing.T, ts *httptest.Server, query string, headers map[string]string) (workersReply, int) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/relay/harness/workers?"+query, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out workersReply
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
	}
	return out, resp.StatusCode
}

func sendRegister(t *testing.T, ws *websocket.Conn, setups any) {
	t.Helper()
	if err := ws.WriteJSON(map[string]any{
		"type": "harness_register",
		"capabilities": map[string]any{
			"tier": "heavy", "providers": []string{"claude", "cursor"}, "max_concurrent": 1, "setups": setups,
		},
	}); err != nil {
		t.Fatal(err)
	}
}

// waitWorkers polls the list until check passes: the register frame is read asynchronously.
func waitWorkers(t *testing.T, ts *httptest.Server, query string, check func(workersReply) bool) workersReply {
	t.Helper()
	var last workersReply
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		last, _ = listWorkers(t, ts, query, internalAuth)
		if check(last) {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("workers never matched; last reply: %+v", last)
	return last
}

func TestHarnessWorkers_listsAdvertisedAgentsAndReplacesThemOnReRegister(t *testing.T) {
	h, ts := newAuthRelay(t, nil, relay.ServerConfig{AuthToken: "test-static", InternalToken: "test-internal"})
	ws, _ := dialHarness(t, ts, "test-static", "tenant_id=tenant-a&user_id=ua&worker_id=workbench:ua&tier=heavy")
	waitHarness(t, h, "tenant-a", "workbench:ua")

	sendRegister(t, ws, []map[string]any{
		{"id": "claude:default", "label": "Claude", "provider": "claude", "model": "m-1", "access": "full"},
		{
			"id": "reviewer-setup", "agent_id": "reviewer", "kind": "agent", "label": "Reviewer", "provider": "cursor",
			"access": "read", "network": false, "web": true, "connection_name": "Team Cursor", "description": "Reviews diffs",
		},
	})
	got := waitWorkers(t, ts, "tenant_id=tenant-a", func(r workersReply) bool {
		return len(r.Workers) == 1 && len(r.Workers[0].Agents) == 2
	})
	w := got.Workers[0]
	if w.WorkerID != "workbench:ua" || w.UserID != "ua" || !w.Online || w.Tier != "heavy" {
		t.Fatalf("worker = %+v", w)
	}
	if len(w.Providers) != 2 || w.Providers[0] != "claude" || w.Providers[1] != "cursor" {
		t.Fatalf("providers = %v", w.Providers)
	}
	if w.ConnectedAt == "" || w.LastSeen == "" {
		t.Fatalf("missing times: %+v", w)
	}
	old, rev := w.Agents[0], w.Agents[1]
	if old.ID != "claude:default" || old.Provider != "claude" || old.Model != "m-1" || old.Access != "full" || old.Network != nil || old.Web != nil {
		t.Fatalf("old-shape setup = %+v", old)
	}
	if rev.ID != "reviewer-setup" || rev.AgentID != "reviewer" || rev.Kind != "agent" || rev.Provider != "cursor" || rev.Access != "read" ||
		rev.Network == nil || *rev.Network || rev.Web == nil || !*rev.Web || rev.ConnectionName != "Team Cursor" || rev.Description != "Reviews diffs" {
		t.Fatalf("specialized agent = %+v", rev)
	}

	// the next harness_register replaces the whole list
	sendRegister(t, ws, []map[string]any{{"id": "codex:default", "provider": "codex", "access": "edit"}})
	waitWorkers(t, ts, "tenant_id=tenant-a", func(r workersReply) bool {
		return len(r.Workers) == 1 && len(r.Workers[0].Agents) == 1 && r.Workers[0].Agents[0].ID == "codex:default"
	})

	// a malformed setups value empties the list but costs the frame nothing else
	sendRegister(t, ws, "not-an-array")
	waitWorkers(t, ts, "tenant_id=tenant-a", func(r workersReply) bool {
		return len(r.Workers) == 1 && len(r.Workers[0].Agents) == 0 && r.Workers[0].Tier == "heavy" && len(r.Workers[0].Providers) == 2
	})
}

func TestHarnessWorkers_keepsLastSeenWhenOfflineAndRefillsOnReconnect(t *testing.T) {
	h, ts := newAuthRelay(t, nil, relay.ServerConfig{AuthToken: "test-static", InternalToken: "test-internal"})
	ws, _ := dialHarness(t, ts, "test-static", "tenant_id=tenant-a&user_id=ua&worker_id=workbench:ua&tier=heavy")
	waitHarness(t, h, "tenant-a", "workbench:ua")
	sendRegister(t, ws, []map[string]any{{"id": "claude:default", "provider": "claude"}})
	waitWorkers(t, ts, "tenant_id=tenant-a", func(r workersReply) bool { return len(r.Workers) == 1 && len(r.Workers[0].Agents) == 1 })

	_ = ws.Close()
	got := waitWorkers(t, ts, "tenant_id=tenant-a", func(r workersReply) bool { return len(r.Workers) == 1 && !r.Workers[0].Online })
	if len(got.Workers[0].Agents) != 1 || got.Workers[0].Agents[0].ID != "claude:default" || got.Workers[0].LastSeen == "" {
		t.Fatalf("offline worker lost its last advertisement: %+v", got.Workers[0])
	}

	// back online: the old list stays until the new register replaces it
	ws2, _ := dialHarness(t, ts, "test-static", "tenant_id=tenant-a&user_id=ua&worker_id=workbench:ua&tier=heavy")
	got = waitWorkers(t, ts, "tenant_id=tenant-a", func(r workersReply) bool { return len(r.Workers) == 1 && r.Workers[0].Online })
	if len(got.Workers[0].Agents) != 1 {
		t.Fatalf("reconnect dropped the agents before the new register: %+v", got.Workers[0])
	}
	sendRegister(t, ws2, []map[string]any{{"id": "dsh:default", "provider": "dsh"}})
	waitWorkers(t, ts, "tenant_id=tenant-a", func(r workersReply) bool {
		return len(r.Workers) == 1 && r.Workers[0].Online && len(r.Workers[0].Agents) == 1 && r.Workers[0].Agents[0].ID == "dsh:default"
	})
}

func TestHarnessWorkers_tenantAndUserIsolation(t *testing.T) {
	h, ts := newAuthRelay(t, nil, relay.ServerConfig{AuthToken: "test-static", InternalToken: "test-internal"})
	dial := func(query, workerID, tenant string) *websocket.Conn {
		ws, status := dialHarness(t, ts, "test-static", query)
		if status != 0 {
			t.Fatalf("dial %s: %d", workerID, status)
		}
		waitHarness(t, h, tenant, workerID)
		return ws
	}
	wsAlice := dial("tenant_id=tenant-a&user_id=ua&worker_id=workbench:ua&tier=heavy", "workbench:ua", "tenant-a")
	wsBob := dial("tenant_id=tenant-a&user_id=ub&worker_id=workbench:ub&tier=heavy", "workbench:ub", "tenant-a")
	wsShared := dial("tenant_id=tenant-a&worker_id=shared-1&tier=lightweight", "shared-1", "tenant-a")
	wsOther := dial("tenant_id=tenant-b&user_id=ua&worker_id=workbench:ua&tier=heavy", "workbench:ua", "tenant-b")
	sendRegister(t, wsAlice, []map[string]any{{"id": "alice-agent"}})
	sendRegister(t, wsBob, []map[string]any{{"id": "bob-agent"}})
	sendRegister(t, wsShared, []map[string]any{{"id": "shared-agent"}})
	sendRegister(t, wsOther, []map[string]any{{"id": "other-tenant-agent"}})

	ids := func(r workersReply) string {
		var out []string
		for _, w := range r.Workers {
			for _, a := range w.Agents {
				out = append(out, w.WorkerID+"/"+a.ID)
			}
		}
		return strings.Join(out, ",")
	}

	all := waitWorkers(t, ts, "tenant_id=tenant-a", func(r workersReply) bool { return strings.Count(ids(r), "/") >= 3 })
	if got := ids(all); got != "workbench:ua/alice-agent,workbench:ub/bob-agent,shared-1/shared-agent" &&
		got != "shared-1/shared-agent,workbench:ua/alice-agent,workbench:ub/bob-agent" {
		t.Fatalf("tenant-a list = %q", got)
	}
	for _, w := range all.Workers {
		if w.TenantID != "tenant-a" {
			t.Fatalf("another tenant's worker in the list: %+v", w)
		}
	}

	mine, status := listWorkers(t, ts, "tenant_id=tenant-a&user_id=ua", internalAuth)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if got := ids(mine); strings.Contains(got, "bob") || !strings.Contains(got, "workbench:ua/alice-agent") || !strings.Contains(got, "shared-1/shared-agent") {
		t.Fatalf("alice sees %q: must be her own and the shared worker, never bob's", got)
	}

	theirs, _ := listWorkers(t, ts, "tenant_id=tenant-b", internalAuth)
	if got := ids(theirs); got != "workbench:ua/other-tenant-agent" {
		t.Fatalf("tenant-b list = %q", got)
	}
	if empty, _ := listWorkers(t, ts, "tenant_id=tenant-c", internalAuth); len(empty.Workers) != 0 {
		t.Fatalf("unknown tenant lists %d workers", len(empty.Workers))
	}
}

func TestHarnessWorkers_requiresInternalTokenAndTenant(t *testing.T) {
	h, ts := newAuthRelay(t, nil, relay.ServerConfig{InternalToken: "test-internal"})
	registerWorker(h, "tenant-a", "workbench:ua", &sink{})

	cases := []struct {
		name    string
		query   string
		headers map[string]string
		want    int
	}{
		{"no token", "tenant_id=tenant-a", nil, http.StatusUnauthorized},
		{"wrong token", "tenant_id=tenant-a", map[string]string{"X-Internal-Token": "nope"}, http.StatusUnauthorized},
		{"bearer is not the internal token", "tenant_id=tenant-a", map[string]string{"Authorization": "Bearer test-internal"}, http.StatusUnauthorized},
		{"token but no tenant", "", internalAuth, http.StatusBadRequest},
		{"token but blank tenant", "tenant_id=%20", internalAuth, http.StatusBadRequest},
		{"valid", "tenant_id=tenant-a", internalAuth, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, got := listWorkers(t, ts, tc.query, tc.headers); got != tc.want {
				t.Fatalf("status = %d, want %d", got, tc.want)
			}
		})
	}

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/relay/harness/workers?tenant_id=tenant-a", nil)
	req.Header.Set("X-Internal-Token", "test-internal")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST: status = %d, want 405", resp.StatusCode)
	}
}

func TestHarnessWorkers_noInternalTokenConfiguredFailsClosed(t *testing.T) {
	_, ts := newAuthRelay(t, nil, relay.ServerConfig{})
	if _, got := listWorkers(t, ts, "tenant_id=tenant-a", nil); got != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", got)
	}
}

// The legacy window is for old dispatch callers; it must not open the new list.
func TestHarnessWorkers_legacyWindowDoesNotOpenIt(t *testing.T) {
	h, ts := newAuthRelay(t, nil, relay.ServerConfig{InternalToken: "test-internal", AllowLegacyDispatch: true})
	registerWorker(h, "tenant-a", "workbench:ua", &sink{})
	if _, got := listWorkers(t, ts, "tenant_id=tenant-a", nil); got != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", got)
	}
	if _, got := listWorkers(t, ts, "tenant_id=tenant-a", internalAuth); got != http.StatusOK {
		t.Fatalf("with the token: status = %d, want 200", got)
	}
}

func TestHarnessWorkers_insecureFlagStillNeedsATenant(t *testing.T) {
	_, ts := newAuthRelay(t, nil, relay.ServerConfig{AllowInsecure: true})
	if _, got := listWorkers(t, ts, "", nil); got != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", got)
	}
}

// /relay/harness/workers (the list) and /relay/harness/workers/{id} (dispatch) share a prefix.
func TestHarnessWorkers_listAndDispatchRoutesCoexist(t *testing.T) {
	h, ts := newAuthRelay(t, nil, relay.ServerConfig{InternalToken: "test-internal"})
	var s sink
	registerWorker(h, "tenant-a", "workbench:ua", &s)
	good := map[string]string{"X-Internal-Token": "test-internal", "X-Tenant-Id": "tenant-a"}
	if got := dispatchRequest(t, ts, "workbench:ua", good, `{"run_id":"r:s"}`).StatusCode; got != http.StatusAccepted {
		t.Fatalf("dispatch: status = %d", got)
	}
	if _, got := listWorkers(t, ts, "tenant_id=tenant-a", internalAuth); got != http.StatusOK {
		t.Fatalf("list: status = %d", got)
	}
}
