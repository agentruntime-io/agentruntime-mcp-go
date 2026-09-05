package relay

import (
	"strings"
	"sync"
	"time"
)

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

// Hub tracks live relay connections keyed by instance_id or worker_id.
type Hub struct {
	mu sync.RWMutex
	mcp           map[string]*Conn
	harness       map[string]*Conn
	harnessByUser map[string]string
	onWorkerOnline func(workerID, tenantID, userID string)
}

func NewHub() *Hub {
	return &Hub{
		mcp:           make(map[string]*Conn),
		harness:       make(map[string]*Conn),
		harnessByUser: make(map[string]string),
	}
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

func (h *Hub) RegisterHarness(workerID string, c *Conn) {
	if workerID == "" || c == nil {
		return
	}
	h.mu.Lock()
	c.WorkerID = workerID
	c.Kind = "harness"
	c.LastSeen = time.Now()
	h.harness[workerID] = c
	if c.TenantID != "" && c.UserID != "" && strings.EqualFold(c.Tier, "heavy") {
		h.harnessByUser[c.TenantID+"#"+c.UserID] = workerID
	}
	fn := h.onWorkerOnline
	h.mu.Unlock()
	if fn != nil {
		fn(workerID, c.TenantID, c.UserID)
	}
}

func (h *Hub) UpdateHarnessCapabilities(workerID string, tier string, providers []string, maxConcurrent int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c, ok := h.harness[workerID]
	if !ok || c == nil {
		return
	}
	c.Tier = strings.ToLower(strings.TrimSpace(tier))
	c.Providers = append([]string(nil), providers...)
	if maxConcurrent > 0 {
		c.MaxConcurrent = maxConcurrent
	} else if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = 1
	}
	if c.TenantID != "" && c.UserID != "" && strings.EqualFold(c.Tier, "heavy") {
		h.harnessByUser[c.TenantID+"#"+c.UserID] = workerID
	}
}

func (h *Hub) IncHarnessActiveJobs(workerID string, delta int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.harness[workerID]; ok && c != nil {
		c.ActiveJobs += delta
		if c.ActiveJobs < 0 {
			c.ActiveJobs = 0
		}
	}
}

func (h *Hub) UnregisterMCP(instanceID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.mcp, instanceID)
}

func (h *Hub) UnregisterHarness(workerID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.harness[workerID]; ok {
		if c.TenantID != "" && c.UserID != "" {
			delete(h.harnessByUser, c.TenantID+"#"+c.UserID)
		}
	}
	delete(h.harness, workerID)
}

func (h *Hub) MCPOnline(instanceID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.mcp[instanceID]
	return ok
}

func (h *Hub) HarnessOnline(workerID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.harness[workerID]
	return ok
}

func (h *Hub) HarnessForUser(tenantID, userID string) (*Conn, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	wid, ok := h.harnessByUser[tenantID+"#"+userID]
	if !ok {
		return nil, false
	}
	c, ok := h.harness[wid]
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

func (h *Hub) GetHarness(workerID string) (*Conn, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c, ok := h.harness[workerID]
	return c, ok
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
