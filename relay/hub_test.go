package relay_test

import (
	"testing"

	"github.com/agentruntime-io/agentruntime-mcp-go/relay"
)

func TestHubHarnessForUser(t *testing.T) {
	h := relay.NewHub()
	conn := &relay.Conn{TenantID: "t1", UserID: "u1", Tier: "heavy", Send: func([]byte) error { return nil }}
	h.RegisterHarness("workbench:u1", conn)
	got, ok := h.HarnessForUser("t1", "u1")
	if !ok || got.WorkerID != "workbench:u1" {
		t.Fatalf("expected workbench worker for user, got ok=%v worker=%q", ok, got.WorkerID)
	}
	if h.HarnessOnline("workbench:u1") != true {
		t.Fatal("expected worker online")
	}
	h.UnregisterHarness("workbench:u1")
	if h.HarnessOnline("workbench:u1") {
		t.Fatal("expected worker offline after unregister")
	}
}

func TestPickSharedWorker_leastLoaded(t *testing.T) {
	h := relay.NewHub()
	w1 := &relay.Conn{
		WorkerID: "shared:1", TenantID: "t1", Tier: "lightweight",
		Providers: []string{"codex"}, MaxConcurrent: 2, ActiveJobs: 1,
		Send: func([]byte) error { return nil },
	}
	w2 := &relay.Conn{
		WorkerID: "shared:2", TenantID: "t1", Tier: "lightweight",
		Providers: []string{"codex"}, MaxConcurrent: 2, ActiveJobs: 0,
		Send: func([]byte) error { return nil },
	}
	h.RegisterHarness("shared:1", w1)
	h.RegisterHarness("shared:2", w2)

	got, ok := h.PickSharedWorker("t1", "codex", "lightweight")
	if !ok {
		t.Fatal("expected shared worker")
	}
	if got.WorkerID != "shared:2" {
		t.Fatalf("expected least-loaded worker shared:2, got %s", got.WorkerID)
	}
}

func TestPickSharedWorker_skipsAtCapacity(t *testing.T) {
	h := relay.NewHub()
	w1 := &relay.Conn{
		WorkerID: "shared:1", TenantID: "t1", Tier: "lightweight",
		Providers: []string{"codex"}, MaxConcurrent: 1, ActiveJobs: 1,
		Send: func([]byte) error { return nil },
	}
	h.RegisterHarness("shared:1", w1)
	if _, ok := h.PickSharedWorker("t1", "codex", "lightweight"); ok {
		t.Fatal("expected no worker at capacity")
	}
}

func TestRequestMultiplexerRoundTrip(t *testing.T) {
	mux := relay.NewRequestMultiplexer(0)
	ch := mux.Register("req-1")
	go func() {
		mux.Complete("req-1", []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}()
	body, ok := mux.Wait(ch)
	if !ok || len(body) == 0 {
		t.Fatalf("expected response body, ok=%v", ok)
	}
}
