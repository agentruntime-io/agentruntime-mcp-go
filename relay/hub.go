package relay

import (
	"strings"
	"sync"
	"time"
)

// dispatchTTL bounds how long a dispatched run_id stays valid for harness_complete.
// It covers the longest harness job (timeout_s defaults to 3600) plus a reconnect.
const dispatchTTL = 6 * time.Hour

// Conn represents a live WebSocket session (MCP instance or harness worker).
type Conn struct {
	ID            string
	Kind          string // "mcp" | "harness"
	InstanceID    string
	WorkerID      string
	TenantID      string
	UserID        string
	Provider      string
	Tier          string   // harness: heavy | lightweight
	Providers     []string // harness: supported providers
	MaxConcurrent int
	ActiveJobs    int
	Send          func([]byte) error
	LastSeen      time.Time
	Mux           *RequestMultiplexer
}

// Hub tracks live relay connections keyed by instance_id or (tenant_id, worker_id).
type Hub struct {
	mu            sync.RWMutex
	mcp           map[string]*Conn
	harness       map[string]*Conn // key: harnessKey(tenant, worker)
	harnessByUser map[string]string
	// harnessWorkers is the registry of what each worker advertised; it keeps a
	// worker's last advertisement after it disconnects (see harness_registry.go).
	harnessWorkers map[string]*harnessWorkerRecord
	// dispatched holds the run_ids sent to a worker, so harness_complete is
	// only accepted for runs that were dispatched to that (tenant, worker).
	dispatched     map[string]map[string]time.Time
	onWorkerOnline func(workerID, tenantID, userID string)
}

func NewHub() *Hub {
	return &Hub{
		mcp:            make(map[string]*Conn),
		harness:        make(map[string]*Conn),
		harnessByUser:  make(map[string]string),
		harnessWorkers: make(map[string]*harnessWorkerRecord),
		dispatched:     make(map[string]map[string]time.Time),
	}
}

// harnessKey qualifies a worker id with its tenant: ids such as workbench:{user_id}
// are chosen by clients, so two tenants may present the same one.
func harnessKey(tenantID, workerID string) string {
	return tenantID + "\x00" + workerID
}

func connHarnessKey(c *Conn) string {
	return harnessKey(c.TenantID, c.WorkerID)
}

func (h *Hub) SetOnWorkerOnline(fn func(workerID, tenantID, userID string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onWorkerOnline = fn
}

func (h *Hub) RegisterMCP(instanceID string, c *Conn) {
	if instanceID == "" || c == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	c.InstanceID = instanceID
	c.Kind = "mcp"
	c.LastSeen = time.Now()
	if c.Mux == nil {
		c.Mux = NewRequestMultiplexer(0)
	}
	h.mcp[instanceID] = c
}

// MCPHeldByOtherTenant reports whether instanceID is live for a tenant other than tenantID.
func (h *Hub) MCPHeldByOtherTenant(instanceID, tenantID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	cur, ok := h.mcp[instanceID]
	return ok && cur != nil && cur.TenantID != "" && tenantID != "" && cur.TenantID != tenantID
}

// TryRegisterMCP registers c unless another tenant's live connection already
// holds instanceID; a later connect must not take over that tenant's connector.
func (h *Hub) TryRegisterMCP(instanceID string, c *Conn) bool {
	if instanceID == "" || c == nil {
		return false
	}
	h.mu.Lock()
	if cur, ok := h.mcp[instanceID]; ok && cur != nil && cur.TenantID != "" && c.TenantID != "" && cur.TenantID != c.TenantID {
		h.mu.Unlock()
		return false
	}
	h.mu.Unlock()
	h.RegisterMCP(instanceID, c)
	return true
}

func (h *Hub) RegisterHarness(workerID string, c *Conn) {
	if workerID == "" || c == nil {
		return
	}
	h.mu.Lock()
	c.WorkerID = workerID
	c.Kind = "harness"
	c.LastSeen = time.Now()
	h.harness[connHarnessKey(c)] = c
	h.touchHarnessRecordLocked(c, c.LastSeen)
	if c.TenantID != "" && c.UserID != "" && strings.EqualFold(c.Tier, "heavy") {
		h.harnessByUser[c.TenantID+"#"+c.UserID] = workerID
	}
	fn := h.onWorkerOnline
	h.mu.Unlock()
	if fn != nil {
		fn(workerID, c.TenantID, c.UserID)
	}
}

// UpdateHarnessCapabilities records what the connection advertised in harness_register.
func (h *Hub) UpdateHarnessCapabilities(c *Conn, tier string, providers []string, maxConcurrent int) {
	if c == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.harness[connHarnessKey(c)]; !ok || cur != c {
		return
	}
	c.Tier = strings.ToLower(strings.TrimSpace(tier))
	c.Providers = append([]string(nil), providers...)
	if maxConcurrent > 0 {
		c.MaxConcurrent = maxConcurrent
	} else if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = 1
	}
	if rec := h.harnessWorkers[connHarnessKey(c)]; rec != nil && rec.conn == c {
		rec.tier = c.Tier
		rec.providers = sanitizeProviders(c.Providers)
		rec.maxConcurrent = c.MaxConcurrent
	}
	if c.TenantID != "" && c.UserID != "" && strings.EqualFold(c.Tier, "heavy") {
		h.harnessByUser[c.TenantID+"#"+c.UserID] = c.WorkerID
	}
}

func (h *Hub) IncHarnessActiveJobs(c *Conn, delta int) {
	if c == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.harness[connHarnessKey(c)]; ok && cur != nil {
		cur.ActiveJobs += delta
		if cur.ActiveJobs < 0 {
			cur.ActiveJobs = 0
		}
	}
}

func (h *Hub) UnregisterMCP(instanceID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.mcp, instanceID)
}

// UnregisterMCPConn removes instanceID only while c is still the registered
// connection, so a late close of a replaced session cannot evict its successor.
func (h *Hub) UnregisterMCPConn(instanceID string, c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.mcp[instanceID]; ok && cur == c {
		delete(h.mcp, instanceID)
	}
}

// UnregisterHarness removes c, but only while c is still the connection
// registered for its (tenant, worker): a reconnect replaces the old session,
// and the old session closing later must not evict the new one.
func (h *Hub) UnregisterHarness(c *Conn) {
	if c == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	key := connHarnessKey(c)
	cur, ok := h.harness[key]
	if !ok || cur != c {
		return
	}
	if c.TenantID != "" && c.UserID != "" {
		delete(h.harnessByUser, c.TenantID+"#"+c.UserID)
	}
	delete(h.harness, key)
	h.markHarnessOfflineLocked(c, time.Now())
}

func (h *Hub) MCPOnline(instanceID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.mcp[instanceID]
	return ok
}

func (h *Hub) HarnessOnline(tenantID, workerID string) bool {
	_, ok := h.GetHarness(tenantID, workerID)
	return ok
}

func (h *Hub) HarnessForUser(tenantID, userID string) (*Conn, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	wid, ok := h.harnessByUser[tenantID+"#"+userID]
	if !ok {
		return nil, false
	}
	c, ok := h.harness[harnessKey(tenantID, wid)]
	return c, ok
}

// PickSharedWorker selects the least-loaded lightweight harness worker for tenant+provider.
func (h *Hub) PickSharedWorker(tenantID, provider, tier string) (*Conn, bool) {
	tenantID = strings.TrimSpace(tenantID)
	provider = strings.ToLower(strings.TrimSpace(provider))
	tier = strings.ToLower(strings.TrimSpace(tier))
	if tier == "" {
		tier = "lightweight"
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	var best *Conn
	bestLoad := int(^uint(0) >> 1)
	for _, c := range h.harness {
		if c == nil || !strings.EqualFold(c.Tier, tier) {
			continue
		}
		if tenantID != "" && c.TenantID != "" && c.TenantID != tenantID {
			continue
		}
		if provider != "" && !harnessSupportsProvider(c, provider) {
			continue
		}
		maxC := c.MaxConcurrent
		if maxC <= 0 {
			maxC = 1
		}
		if c.ActiveJobs >= maxC {
			continue
		}
		load := c.ActiveJobs
		if best == nil || load < bestLoad {
			best = c
			bestLoad = load
		}
	}
	if best == nil {
		return nil, false
	}
	return best, true
}

func harnessSupportsProvider(c *Conn, provider string) bool {
	if c == nil || provider == "" {
		return false
	}
	if len(c.Providers) == 0 {
		if c.Provider == "" {
			return true
		}
		for _, p := range strings.Split(c.Provider, ",") {
			if strings.EqualFold(strings.TrimSpace(p), provider) {
				return true
			}
		}
		return false
	}
	for _, p := range c.Providers {
		if strings.EqualFold(strings.TrimSpace(p), provider) {
			return true
		}
	}
	return false
}

func (h *Hub) GetMCP(instanceID string) (*Conn, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c, ok := h.mcp[instanceID]
	return c, ok
}

// GetHarness returns the worker connected for tenantID. A worker that connected
// without a tenant (shared pool, static dev token) serves every tenant, so it is
// the fallback when the tenant has no worker of that id.
func (h *Hub) GetHarness(tenantID, workerID string) (*Conn, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if c, ok := h.harness[harnessKey(tenantID, workerID)]; ok {
		return c, true
	}
	if tenantID != "" {
		if c, ok := h.harness[harnessKey("", workerID)]; ok {
			return c, true
		}
	}
	return nil, false
}

// GetHarnessAnyTenant finds workerID when the caller did not name a tenant (the
// legacy dispatch shape). It only answers when exactly one tenant has that id.
func (h *Hub) GetHarnessAnyTenant(workerID string) (*Conn, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var found *Conn
	for _, c := range h.harness {
		if c == nil || c.WorkerID != workerID {
			continue
		}
		if found != nil {
			return nil, false
		}
		found = c
	}
	return found, found != nil
}

// TrackDispatch records that runID was sent to c, so its harness_complete is accepted.
func (h *Hub) TrackDispatch(c *Conn, runID string) {
	if c == nil || runID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	key := connHarnessKey(c)
	runs := h.dispatched[key]
	if runs == nil {
		runs = make(map[string]time.Time)
		h.dispatched[key] = runs
	}
	for id, at := range runs {
		if now.Sub(at) > dispatchTTL {
			delete(runs, id)
		}
	}
	runs[runID] = now
}

// ForgetDispatch undoes TrackDispatch when the job could not be sent.
func (h *Hub) ForgetDispatch(c *Conn, runID string) {
	if c == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	key := connHarnessKey(c)
	if runs := h.dispatched[key]; runs != nil {
		delete(runs, runID)
		if len(runs) == 0 {
			delete(h.dispatched, key)
		}
	}
}

// ConsumeDispatch reports whether runID was dispatched to c's (tenant, worker)
// and has not completed yet, and marks it completed. The record is keyed by
// worker identity rather than socket, so a worker that reconnects mid-job can
// still deliver its result.
func (h *Hub) ConsumeDispatch(c *Conn, runID string) bool {
	if c == nil || runID == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	key := connHarnessKey(c)
	runs := h.dispatched[key]
	at, ok := runs[runID]
	if !ok {
		return false
	}
	delete(runs, runID)
	if len(runs) == 0 {
		delete(h.dispatched, key)
	}
	return time.Since(at) <= dispatchTTL
}

func (h *Hub) Touch(conn *Conn) {
	if conn == nil {
		return
	}
	h.mu.Lock()
	conn.LastSeen = time.Now()
	h.mu.Unlock()
}

var DefaultHub = NewHub()
