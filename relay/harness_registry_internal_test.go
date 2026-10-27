package relay

import (
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// White-box tests for the registry's bounds and eviction, which need the unexported limits.

func quietConn(tenant, user string) *Conn {
	return &Conn{TenantID: tenant, UserID: user, Tier: "heavy", Send: func([]byte) error { return nil }}
}

func TestSanitizeHarnessAgents_boundsAndDedupes(t *testing.T) {
	yes := true
	long := strings.Repeat("é", 2000) // two bytes a rune: must be cut on a rune boundary
	in := []HarnessAgent{
		{ID: "  a1  ", Label: " Agent One ", Provider: " CLAUDE ", Access: "Read", Web: &yes, Description: long},
		{ID: ""},               // no id: dropped
		{ID: "   "},            // blank id: dropped
		{ID: "a1", Label: "x"}, // repeat of (kind, id): dropped
		{ID: "a1", Kind: "agent", AgentID: "own"},
	}
	for i := 0; i < 100; i++ {
		in = append(in, HarnessAgent{ID: "bulk-" + strconv.Itoa(i)})
	}
	out := sanitizeHarnessAgents(in)
	if len(out) != maxHarnessAgentsPerWorker {
		t.Fatalf("kept %d agents, want the cap of %d", len(out), maxHarnessAgentsPerWorker)
	}
	if out[0].ID != "a1" || out[0].Label != "Agent One" || out[0].Provider != "claude" || out[0].Access != "read" || out[0].Web == nil || !*out[0].Web {
		t.Fatalf("first agent = %+v", out[0])
	}
	if d := out[0].Description; d == "" || len(d) > maxAgentDescriptionLen || !utf8.ValidString(d) {
		t.Fatalf("description not bounded on a rune boundary: %d bytes, valid=%v", len(d), utf8.ValidString(d))
	}
	if out[1].ID != "a1" || out[1].Kind != "agent" || out[1].AgentID != "own" {
		t.Fatalf("(kind, id) handling: %+v", out[1])
	}
	// the stored pointer is not the caller's
	*in[0].Web = false
	if !*out[0].Web {
		t.Fatal("sanitized agent shares its Web pointer with the input")
	}
}

func TestParseHarnessSetups_toleratesJunk(t *testing.T) {
	for _, raw := range []string{``, `null`, `"text"`, `42`, `{"id":"x"}`, `[1, "two", null]`} {
		if got := parseHarnessSetups([]byte(raw)); len(got) != 0 {
			t.Fatalf("%q parsed to %+v", raw, got)
		}
	}
	got := parseHarnessSetups([]byte(`[7, {"id":"ok","network":"yes"}, {"id":"fine","network":false}]`))
	// the entry with a wrong-typed field is skipped, the rest are kept
	if len(got) != 1 || got[0].ID != "fine" || got[0].Network == nil || *got[0].Network {
		t.Fatalf("got %+v", got)
	}
}

func TestHubListHarnessWorkers_copiesWhatItReturns(t *testing.T) {
	h := NewHub()
	c := quietConn("t1", "u1")
	h.RegisterHarness("workbench:u1", c)
	yes := true
	h.SetHarnessAgents(c, []HarnessAgent{{ID: "a", Label: "Agent", Web: &yes}})

	got := h.ListHarnessWorkers("t1", "")
	*got[0].Agents[0].Web = false
	got[0].Agents[0].Label = "changed"
	got[0].Providers = append(got[0].Providers, "x")
	again := h.ListHarnessWorkers("t1", "")
	if again[0].Agents[0].Label != "Agent" || !*again[0].Agents[0].Web {
		t.Fatalf("registry shares memory with the list: %+v", again[0].Agents[0])
	}
}

func TestHubListHarnessWorkers_tenantRequiredAndSharedPoolExcluded(t *testing.T) {
	h := NewHub()
	h.RegisterHarness("pool-1", &Conn{Tier: "lightweight", Send: func([]byte) error { return nil }}) // no tenant: serves any tenant
	h.RegisterHarness("w", quietConn("t1", ""))
	if got := h.ListHarnessWorkers("", ""); len(got) != 0 {
		t.Fatalf("an empty tenant listed %d workers", len(got))
	}
	got := h.ListHarnessWorkers("t1", "")
	if len(got) != 1 || got[0].WorkerID != "w" {
		t.Fatalf("t1 list = %+v: the tenant-less shared pool belongs to no tenant's list", got)
	}
}

func TestHubListHarnessWorkers_userFilterKeepsOwnAndSharedOnly(t *testing.T) {
	h := NewHub()
	h.RegisterHarness("workbench:ua", quietConn("t1", "ua"))
	h.RegisterHarness("workbench:ub", quietConn("t1", "ub"))
	h.RegisterHarness("shared-1", quietConn("t1", ""))
	var ids []string
	for _, w := range h.ListHarnessWorkers("t1", "ua") {
		ids = append(ids, w.WorkerID)
	}
	if strings.Join(ids, ",") != "shared-1,workbench:ua" && strings.Join(ids, ",") != "workbench:ua,shared-1" {
		t.Fatalf("ua sees %v", ids)
	}
	if n := len(h.ListHarnessWorkers("t1", "")); n != 3 {
		t.Fatalf("no user filter lists %d workers, want all 3", n)
	}
}

func TestHubListHarnessWorkers_onlineFirstAndStaleSweepKeepsLastSeen(t *testing.T) {
	h := NewHub()
	a, b := quietConn("t1", "u"), quietConn("t1", "u")
	h.RegisterHarness("a-worker", a)
	h.RegisterHarness("b-worker", b)
	h.SetHarnessAgents(a, []HarnessAgent{{ID: "agent-a"}})
	h.UnregisterHarness(a)

	got := h.ListHarnessWorkers("t1", "u")
	if len(got) != 2 || got[0].WorkerID != "b-worker" || !got[0].Online || got[1].WorkerID != "a-worker" || got[1].Online {
		t.Fatalf("order = %+v", got)
	}
	if len(got[1].Agents) != 1 || got[1].LastSeen.IsZero() {
		t.Fatalf("offline worker = %+v", got[1])
	}

	// a silent connection is evicted by the heartbeat sweep and stays listed as offline
	h.mu.Lock()
	b.LastSeen = time.Now().Add(-10 * time.Minute)
	h.mu.Unlock()
	h.sweepStale(nil, nil)
	got = h.ListHarnessWorkers("t1", "u")
	if len(got) != 2 || got[0].Online || got[1].Online {
		t.Fatalf("after the sweep = %+v", got)
	}
}

func TestHubSetHarnessAgents_ignoresAReplacedConnection(t *testing.T) {
	h := NewHub()
	oldConn, newConn := quietConn("t1", "u"), quietConn("t1", "u")
	h.RegisterHarness("w", oldConn)
	h.RegisterHarness("w", newConn) // reconnect
	h.SetHarnessAgents(newConn, []HarnessAgent{{ID: "fresh"}})
	h.SetHarnessAgents(oldConn, []HarnessAgent{{ID: "stale"}}) // a late frame of the dead socket
	h.UnregisterHarness(oldConn)                               // and its late close

	got := h.ListHarnessWorkers("t1", "")
	if len(got) != 1 || !got[0].Online || len(got[0].Agents) != 1 || got[0].Agents[0].ID != "fresh" {
		t.Fatalf("list = %+v", got)
	}
}

func TestHubHarnessWorkers_tenantCapEvictsLongestOffline(t *testing.T) {
	h := NewHub()
	h.RegisterHarness("live", quietConn("t1", ""))
	h.RegisterHarness("other-tenant", quietConn("t2", ""))
	for i := 0; i < maxHarnessWorkersPerTenant+10; i++ {
		c := quietConn("t1", "")
		h.RegisterHarness("w-"+strconv.Itoa(i), c)
		h.mu.Lock()
		c.LastSeen = time.Now().Add(time.Duration(i-1000) * time.Second) // earlier index = longer offline
		h.mu.Unlock()
		h.UnregisterHarness(c)
	}
	got := h.ListHarnessWorkers("t1", "")
	if len(got) > maxHarnessWorkersPerTenant {
		t.Fatalf("t1 keeps %d records, cap is %d", len(got), maxHarnessWorkersPerTenant)
	}
	if !got[0].Online || got[0].WorkerID != "live" {
		t.Fatalf("the live worker must survive the cap: %+v", got[0])
	}
	for _, w := range got {
		if w.WorkerID == "w-0" {
			t.Fatal("the longest-offline worker should have been evicted first")
		}
	}
	if o := h.ListHarnessWorkers("t2", ""); len(o) != 1 {
		t.Fatalf("another tenant's worker was evicted: %+v", o)
	}
}

func TestHubHarnessWorkers_offlineRecordExpires(t *testing.T) {
	h := NewHub()
	gone := quietConn("t1", "")
	h.RegisterHarness("gone", gone)
	h.UnregisterHarness(gone)
	h.mu.Lock()
	h.harnessWorkers[connHarnessKey(gone)].lastSeen = time.Now().Add(-harnessWorkerRetention - time.Hour)
	h.mu.Unlock()
	if got := h.ListHarnessWorkers("t1", ""); len(got) != 0 {
		t.Fatalf("an expired offline worker is still listed: %+v", got)
	}
	// and the next connect sweeps it out of memory
	h.RegisterHarness("fresh", quietConn("t1", ""))
	h.mu.RLock()
	n := len(h.harnessWorkers)
	h.mu.RUnlock()
	if n != 1 {
		t.Fatalf("registry holds %d records, want 1", n)
	}
}
