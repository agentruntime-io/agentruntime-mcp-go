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
	if h.HarnessOnline("t1", "workbench:u1") != true {
		t.Fatal("expected worker online")
	}
	h.UnregisterHarness(conn)
	if h.HarnessOnline("t1", "workbench:u1") {
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

func TestHub_harnessIsKeyedByTenantAndWorker(t *testing.T) {
	h := relay.NewHub()
	a := &relay.Conn{TenantID: "tenant-a", UserID: "u", Tier: "heavy", Send: func([]byte) error { return nil }}
	b := &relay.Conn{TenantID: "tenant-b", UserID: "u", Tier: "heavy", Send: func([]byte) error { return nil }}
	h.RegisterHarness("workbench:u", a)
	h.RegisterHarness("workbench:u", b)

	if got, ok := h.GetHarness("tenant-a", "workbench:u"); !ok || got != a {
		t.Fatal("tenant-a's worker was displaced by tenant-b registering the same id")
	}
	if got, ok := h.GetHarness("tenant-b", "workbench:u"); !ok || got != b {
		t.Fatal("tenant-b's worker not found")
	}
	if _, ok := h.GetHarness("tenant-c", "workbench:u"); ok {
		t.Fatal("a tenant with no such worker found one")
	}
	if got, ok := h.HarnessForUser("tenant-a", "u"); !ok || got != a {
		t.Fatal("HarnessForUser crossed tenants")
	}
	h.UnregisterHarness(a)
	if _, ok := h.GetHarness("tenant-b", "workbench:u"); !ok {
		t.Fatal("unregistering tenant-a's worker removed tenant-b's")
	}
}

func TestHub_tenantlessSharedWorkerServesAnyTenant(t *testing.T) {
	h := relay.NewHub()
	shared := &relay.Conn{Tier: "lightweight", Send: func([]byte) error { return nil }}
	h.RegisterHarness("shared:pool", shared)
	if got, ok := h.GetHarness("tenant-a", "shared:pool"); !ok || got != shared {
		t.Fatal("tenant-less shared worker not found for a tenant")
	}
	own := &relay.Conn{TenantID: "tenant-a", Send: func([]byte) error { return nil }}
	h.RegisterHarness("shared:pool", own)
	if got, _ := h.GetHarness("tenant-a", "shared:pool"); got != own {
		t.Fatal("a tenant's own worker must win over the tenant-less one")
	}
}

func TestHub_staleDisconnectDoesNotEvictReconnectedWorker(t *testing.T) {
	h := relay.NewHub()
	old := &relay.Conn{TenantID: "t1", UserID: "u", Tier: "heavy", Send: func([]byte) error { return nil }}
	h.RegisterHarness("workbench:u", old)
	fresh := &relay.Conn{TenantID: "t1", UserID: "u", Tier: "heavy", Send: func([]byte) error { return nil }}
	h.RegisterHarness("workbench:u", fresh) // the daemon reconnected
	h.UnregisterHarness(old)                // the old socket's read loop ends later
	if got, ok := h.GetHarness("t1", "workbench:u"); !ok || got != fresh {
		t.Fatal("the old session's close evicted the reconnected worker")
	}
}

func TestHub_dispatchTracking(t *testing.T) {
	h := relay.NewHub()
	a := &relay.Conn{TenantID: "t1", Send: func([]byte) error { return nil }}
	b := &relay.Conn{TenantID: "t2", Send: func([]byte) error { return nil }}
	h.RegisterHarness("w", a)
	h.RegisterHarness("w", b)

	if h.ConsumeDispatch(a, "run-1") {
		t.Fatal("a run never dispatched was accepted")
	}
	h.TrackDispatch(a, "run-1")
	if h.ConsumeDispatch(b, "run-1") {
		t.Fatal("a run dispatched to tenant t1 was accepted from tenant t2")
	}
	if !h.ConsumeDispatch(a, "run-1") {
		t.Fatal("the dispatched run was refused")
	}
	if h.ConsumeDispatch(a, "run-1") {
		t.Fatal("a run was accepted twice")
	}
	h.TrackDispatch(a, "run-2")
	h.ForgetDispatch(a, "run-2")
	if h.ConsumeDispatch(a, "run-2") {
		t.Fatal("a forgotten dispatch was accepted")
	}
}

func TestHub_mcpOtherTenantCannotTakeLiveInstance(t *testing.T) {
	h := relay.NewHub()
	owner := &relay.Conn{TenantID: "t1", Send: func([]byte) error { return nil }}
	if !h.TryRegisterMCP("inst", owner) {
		t.Fatal("first registration refused")
	}
	if h.TryRegisterMCP("inst", &relay.Conn{TenantID: "t2", Send: func([]byte) error { return nil }}) {
		t.Fatal("another tenant took over a live instance")
	}
	if !h.MCPHeldByOtherTenant("inst", "t2") || h.MCPHeldByOtherTenant("inst", "t1") {
		t.Fatal("MCPHeldByOtherTenant is wrong")
	}
	again := &relay.Conn{TenantID: "t1", Send: func([]byte) error { return nil }}
	if !h.TryRegisterMCP("inst", again) {
		t.Fatal("the owning tenant could not reconnect")
	}
	h.UnregisterMCPConn("inst", owner) // late close of the replaced session
	if got, ok := h.GetMCP("inst"); !ok || got != again {
		t.Fatal("a late close evicted the reconnected instance")
	}
}
