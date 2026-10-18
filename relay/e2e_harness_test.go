package relay_test

import (
	"bytes"
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

type fakeOrchestrator struct {
	mu      sync.Mutex
	online  []map[string]string
	complete []map[string]any
}

func (f *fakeOrchestrator) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/internal/harness/worker-online", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]string
		_ = json.Unmarshal(body, &req)
		f.mu.Lock()
		f.online = append(f.online, req)
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/v1/internal/harness/run-complete", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		f.mu.Lock()
		f.complete = append(f.complete, req)
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	return httptest.NewServer(mux)
}

// TestHarnessE2E_FullRoundTrip covers relay WSS register → HTTP dispatch → harness_complete → orchestrator callback.
func TestHarnessE2E_FullRoundTrip(t *testing.T) {
	orch := &fakeOrchestrator{}
	orchSrv := orch.server()
	defer orchSrv.Close()

	srv := relay.NewServer(relay.ServerConfig{
		AuthToken:            "e2e-token",
		OrchestratorCallback: relay.NewOrchestratorCallback(orchSrv.URL, ""),
	})
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	workerDone := make(chan struct{})
	go runMockHarnessWorker(t, ts, "workbench:e2e-user", "tenant-1", "e2e-user", "heavy", workerDone)
	time.Sleep(100 * time.Millisecond)

	job := map[string]any{
		"run_id":   "run-1:step-codex",
		"provider": "codex",
		"prompt":   "Say hello for E2E",
	}
	raw, _ := json.Marshal(job)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/relay/harness/workers/workbench:e2e-user/run", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("dispatch: %d %s", resp.StatusCode, string(b))
	}

	select {
	case <-workerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("mock worker did not complete job in time")
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		orch.mu.Lock()
		n := len(orch.complete)
		orch.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	orch.mu.Lock()
	defer orch.mu.Unlock()
	if len(orch.complete) != 1 {
		t.Fatalf("expected 1 run-complete callback, got %d (online=%d)", len(orch.complete), len(orch.online))
	}
	if orch.complete[0]["run_id"] != "run-1:step-codex" {
		t.Fatalf("unexpected run_id in callback: %#v", orch.complete[0])
	}
}

// TestHarnessE2E_OfflineDispatchThenReconnect simulates workbench offline queue at relay layer.
func TestHarnessE2E_OfflineDispatchThenReconnect(t *testing.T) {
	srv := relay.NewServer(relay.ServerConfig{AuthToken: "e2e-token"})
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/relay/harness/workers/workbench:offline-user/run", "application/json",
		strings.NewReader(`{"run_id":"run-q:step-1","provider":"codex","prompt":"queued"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when worker offline, got %d", resp.StatusCode)
	}

	workerDone := make(chan struct{})
	go runMockHarnessWorker(t, ts, "workbench:offline-user", "tenant-1", "offline-user", "heavy", workerDone)
	time.Sleep(100 * time.Millisecond)

	resp2, err := http.Post(ts.URL+"/relay/harness/workers/workbench:offline-user/run", "application/json",
		strings.NewReader(`{"run_id":"run-q:step-1","provider":"codex","prompt":"queued"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(resp2.Body)
		t.Fatalf("expected 202 after reconnect, got %d %s", resp2.StatusCode, string(b))
	}
	select {
	case <-workerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish dispatched job")
	}
}

// TestHarnessE2E_SharedWorkerPickAndDispatch exercises Tier 2 lightweight pool routing.
func TestHarnessE2E_SharedWorkerPickAndDispatch(t *testing.T) {
	srv := relay.NewServer(relay.ServerConfig{AuthToken: "e2e-token"})
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	workerDone := make(chan struct{})
	go runMockHarnessWorkerTier(t, ts, "shared:pool-1", "tenant-pool", "", "lightweight", []string{"claude"}, workerDone)
	time.Sleep(100 * time.Millisecond)

	pickResp, err := http.Get(ts.URL + "/relay/harness/pick?tenant_id=tenant-pool&provider=claude&tier=lightweight")
	if err != nil {
		t.Fatal(err)
	}
	defer pickResp.Body.Close()
	if pickResp.StatusCode != http.StatusOK {
		t.Fatalf("pick failed: %d", pickResp.StatusCode)
	}
	var pick map[string]any
	if err := json.NewDecoder(pickResp.Body).Decode(&pick); err != nil {
		t.Fatal(err)
	}
	workerID, _ := pick["worker_id"].(string)
	if workerID != "shared:pool-1" {
		t.Fatalf("expected shared:pool-1, got %q", workerID)
	}

	dispatchURL := ts.URL + "/relay/harness/workers/" + workerID + "/run"
	resp, err := http.Post(dispatchURL, "application/json",
		strings.NewReader(`{"run_id":"run-shared:step-1","provider":"claude","prompt":"pool test"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("dispatch to shared worker: %d", resp.StatusCode)
	}
	select {
	case <-workerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("shared worker did not complete")
	}
}

func runMockHarnessWorker(t *testing.T, ts *httptest.Server, workerID, tenantID, userID, tier string, done chan struct{}) {
	runMockHarnessWorkerTier(t, ts, workerID, tenantID, userID, tier, []string{"codex", "claude"}, done)
}

func runMockHarnessWorkerTier(t *testing.T, ts *httptest.Server, workerID, tenantID, userID, tier string, providers []string, done chan struct{}) {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/harness/connect?worker_id=" + workerID +
		"&tenant_id=" + tenantID + "&user_id=" + userID + "&tier=" + tier
	header := http.Header{}
	header.Set("Authorization", "Bearer e2e-token")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Errorf("worker dial: %v", err)
		close(done)
		return
	}
	defer ws.Close()
	_ = ws.WriteJSON(map[string]any{
		"type":      "harness_register",
		"worker_id": workerID,
		"capabilities": map[string]any{
			"tier":           tier,
			"providers":      providers,
			"max_concurrent": 2,
		},
	})
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			return
		}
		var frame map[string]any
		if err := json.Unmarshal(msg, &frame); err != nil {
			continue
		}
		switch frame["type"] {
		case "ping":
			_ = ws.WriteJSON(map[string]string{"type": "pong"})
		case "harness_run":
			runID, _ := frame["run_id"].(string)
			_ = ws.WriteJSON(map[string]any{
				"type":   "harness_complete",
				"run_id": runID,
				"status": "finished",
				"result": map[string]any{"final_text": "mock ok", "mock": true},
			})
			close(done)
			return
		}
	}
}
