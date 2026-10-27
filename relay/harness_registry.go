package relay

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// The registry keeps what each Harness worker last advertised in
// harness_register, so AgentRuntime can list the agents on a user's machines
// whether or not the machine is online right now. It lives in relay memory
// (decision D5 in HARNESS_WORKER_RELAY.md): a relay restart empties it, and
// workers refill it by reconnecting.

const (
	// harnessWorkerRetention is how long an offline worker stays listed as
	// "last seen" before it is dropped.
	harnessWorkerRetention = 30 * 24 * time.Hour
	// maxHarnessWorkersPerTenant bounds the records one tenant can leave behind:
	// worker ids are chosen by clients, so a tenant member could otherwise fill
	// the relay's memory with ids. Past it, the longest-offline record goes.
	maxHarnessWorkersPerTenant = 256
	// maxHarnessAgentsPerWorker bounds the agents kept from one harness_register.
	maxHarnessAgentsPerWorker = 64
	maxHarnessProviders       = 16

	maxAgentIDLen          = 128
	maxAgentShortFieldLen  = 64
	maxAgentLabelLen       = 200
	maxAgentDescriptionLen = 1000
)

// HarnessAgent is one agent a worker advertises in harness_register
// capabilities.setups[]. Besides the old setup fields (id, label, provider,
// model, access) a worker may send a ceiling summary: network, web, the
// connection's display name and a description. Every field except ID is
// optional, and nothing in it is a secret.
type HarnessAgent struct {
	// ID is what a job names to run this agent (`setup_id`).
	ID string `json:"id"`
	// AgentID is the agent's own id on the worker when ID is a moved setup's id.
	AgentID string `json:"agent_id,omitempty"`
	// Kind is "setup" (a Harness setup) or "agent" (a specialized agent).
	Kind     string `json:"kind,omitempty"`
	Label    string `json:"label,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	// Access is the access level the agent is limited to (read | edit | full).
	Access string `json:"access,omitempty"`
	// Network and Web say whether the agent's commands may reach the network and
	// whether it may use the web. Absent when the worker does not say.
	Network *bool `json:"network,omitempty"`
	Web     *bool `json:"web,omitempty"`
	// ConnectionName is the display name of the login the agent runs as.
	ConnectionName string `json:"connection_name,omitempty"`
	Description    string `json:"description,omitempty"`
}

// HarnessWorkerInfo is a worker as the registry reports it.
type HarnessWorkerInfo struct {
	WorkerID      string         `json:"worker_id"`
	TenantID      string         `json:"tenant_id"`
	UserID        string         `json:"user_id,omitempty"`
	Tier          string         `json:"tier,omitempty"`
	Providers     []string       `json:"providers"`
	MaxConcurrent int            `json:"max_concurrent"`
	ActiveJobs    int            `json:"active_jobs"`
	Online        bool           `json:"online"`
	ConnectedAt   time.Time      `json:"connected_at"`
	LastSeen      time.Time      `json:"last_seen"`
	Agents        []HarnessAgent `json:"agents"`
}

// harnessWorkerRecord is the registry's entry for one (tenant, worker). It
// outlives the connection, so a disconnect leaves the last advertisement behind.
type harnessWorkerRecord struct {
	workerID      string
	tenantID      string
	userID        string
	tier          string
	providers     []string
	maxConcurrent int
	agents        []HarnessAgent
	connectedAt   time.Time
	lastSeen      time.Time // when the worker went offline; a live one reads conn.LastSeen
	conn          *Conn     // the live connection; nil while offline
}

// parseHarnessSetups reads the capabilities.setups array of a harness_register
// frame. A value that is not an array of agents yields none: a malformed list
// must not cost the rest of the frame.
func parseHarnessSetups(raw json.RawMessage) []HarnessAgent {
	if len(raw) == 0 {
		return nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil
	}
	if len(entries) > maxHarnessAgentsPerWorker*2 {
		entries = entries[:maxHarnessAgentsPerWorker*2]
	}
	agents := make([]HarnessAgent, 0, len(entries))
	for _, entry := range entries {
		var a HarnessAgent
		if err := json.Unmarshal(entry, &a); err != nil {
			continue
		}
		agents = append(agents, a)
	}
	return sanitizeHarnessAgents(agents)
}

// sanitizeHarnessAgents trims every field to a sane length, drops entries with
// no id and repeats of one (kind, id), and keeps at most maxHarnessAgentsPerWorker.
// The input comes from a connected client and is stored in relay memory.
func sanitizeHarnessAgents(in []HarnessAgent) []HarnessAgent {
	out := make([]HarnessAgent, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, a := range in {
		a.ID = clip(a.ID, maxAgentIDLen)
		if a.ID == "" {
			continue
		}
		a.AgentID = clip(a.AgentID, maxAgentIDLen)
		a.Kind = strings.ToLower(clip(a.Kind, maxAgentShortFieldLen))
		a.Label = clip(a.Label, maxAgentLabelLen)
		a.Provider = strings.ToLower(clip(a.Provider, maxAgentShortFieldLen))
		a.Model = clip(a.Model, maxAgentIDLen)
		a.Access = strings.ToLower(clip(a.Access, maxAgentShortFieldLen))
		a.ConnectionName = clip(a.ConnectionName, maxAgentLabelLen)
		a.Description = clip(a.Description, maxAgentDescriptionLen)
		key := a.Kind + "\x00" + a.ID
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, copyAgent(a))
		if len(out) == maxHarnessAgentsPerWorker {
			break
		}
	}
	return out
}

// clip trims s and cuts it to at most max bytes, on a rune boundary.
func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimSpace(s[:cut])
}

func sanitizeProviders(in []string) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		p = strings.ToLower(clip(p, maxAgentShortFieldLen))
		if p == "" {
			continue
		}
		out = append(out, p)
		if len(out) == maxHarnessProviders {
			break
		}
	}
	return out
}

// copyAgent returns a with its pointer fields copied, so a stored agent shares
// nothing with the caller.
func copyAgent(a HarnessAgent) HarnessAgent {
	if a.Network != nil {
		v := *a.Network
		a.Network = &v
	}
	if a.Web != nil {
		v := *a.Web
		a.Web = &v
	}
	return a
}

func copyAgents(in []HarnessAgent) []HarnessAgent {
	out := make([]HarnessAgent, len(in))
	for i, a := range in {
		out[i] = copyAgent(a)
	}
	return out
}

// touchHarnessRecordLocked creates or refreshes the record for a worker that
// just connected. The agents the worker advertised before are kept until its
// next harness_register replaces them.
func (h *Hub) touchHarnessRecordLocked(c *Conn, now time.Time) {
	key := connHarnessKey(c)
	rec := h.harnessWorkers[key]
	if rec == nil {
		h.makeRoomForRecordLocked(c.TenantID)
		rec = &harnessWorkerRecord{workerID: c.WorkerID, tenantID: c.TenantID}
		h.harnessWorkers[key] = rec
	}
	rec.userID = c.UserID
	rec.tier = c.Tier
	rec.conn = c
	rec.connectedAt = now
	rec.lastSeen = now
	h.pruneHarnessRecordsLocked(now)
}

// markHarnessOfflineLocked keeps c's record as "last seen" once its
// connection is gone.
func (h *Hub) markHarnessOfflineLocked(c *Conn, now time.Time) {
	rec := h.harnessWorkers[connHarnessKey(c)]
	if rec == nil || rec.conn != c {
		return
	}
	rec.lastSeen = c.LastSeen
	if rec.lastSeen.IsZero() {
		rec.lastSeen = now
	}
	rec.conn = nil
}

// pruneHarnessRecordsLocked drops offline records not seen within harnessWorkerRetention.
func (h *Hub) pruneHarnessRecordsLocked(now time.Time) {
	for key, rec := range h.harnessWorkers {
		if rec.conn == nil && now.Sub(rec.lastSeen) > harnessWorkerRetention {
			delete(h.harnessWorkers, key)
		}
	}
}

// makeRoomForRecordLocked evicts the longest-offline records of tenantID while
// it is at its cap. Live workers are never evicted.
func (h *Hub) makeRoomForRecordLocked(tenantID string) {
	for {
		count := 0
		var oldestKey string
		var oldest time.Time
		for key, rec := range h.harnessWorkers {
			if rec.tenantID != tenantID {
				continue
			}
			count++
			if rec.conn == nil && (oldestKey == "" || rec.lastSeen.Before(oldest)) {
				oldestKey, oldest = key, rec.lastSeen
			}
		}
		if count < maxHarnessWorkersPerTenant || oldestKey == "" {
			return
		}
		delete(h.harnessWorkers, oldestKey)
	}
}

// SetHarnessAgents replaces the agents c advertised. harness_register carries
// the worker's whole list, so a register after a change drops what is gone.
func (h *Hub) SetHarnessAgents(c *Conn, agents []HarnessAgent) {
	if c == nil {
		return
	}
	agents = sanitizeHarnessAgents(agents)
	h.mu.Lock()
	defer h.mu.Unlock()
	rec := h.harnessWorkers[connHarnessKey(c)]
	if rec == nil || rec.conn != c {
		return
	}
	rec.agents = agents
}

// ListHarnessWorkers reports the workers of tenantID, live and last seen.
// With a non-empty userID it leaves out the workers of other users: what is
// left are that user's own and the tenant's shared ones (no user). Live
// workers come first, then by worker id. A worker that connected without a
// tenant (shared pool) belongs to no tenant's list.
func (h *Hub) ListHarnessWorkers(tenantID, userID string) []HarnessWorkerInfo {
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	if tenantID == "" {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	now := time.Now()
	out := make([]HarnessWorkerInfo, 0)
	for _, rec := range h.harnessWorkers {
		if rec.tenantID != tenantID {
			continue
		}
		if userID != "" && rec.userID != "" && rec.userID != userID {
			continue
		}
		if rec.conn == nil && now.Sub(rec.lastSeen) > harnessWorkerRetention {
			continue
		}
		info := HarnessWorkerInfo{
			WorkerID:      rec.workerID,
			TenantID:      rec.tenantID,
			UserID:        rec.userID,
			Tier:          rec.tier,
			Providers:     append([]string{}, rec.providers...),
			MaxConcurrent: rec.maxConcurrent,
			Online:        rec.conn != nil,
			ConnectedAt:   rec.connectedAt,
			LastSeen:      rec.lastSeen,
			Agents:        copyAgents(rec.agents),
		}
		if rec.conn != nil {
			info.ActiveJobs = rec.conn.ActiveJobs
			info.LastSeen = rec.conn.LastSeen
			info.Tier = rec.conn.Tier
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Online != out[j].Online {
			return out[i].Online
		}
		return out[i].WorkerID < out[j].WorkerID
	})
	return out
}
