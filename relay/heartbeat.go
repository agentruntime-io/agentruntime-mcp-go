package relay

import (
	"context"
	"log"
	"time"
)

const (
	defaultHeartbeatInterval = 30 * time.Second
	defaultStaleThreshold    = 90 * time.Second
)

// StartHeartbeatMonitor pings live connections and evicts stale sessions.
func (h *Hub) StartHeartbeatMonitor(ctx context.Context, onMCPOffline, onHarnessOffline func(*Conn, string)) {
	if h == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(defaultHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.sweepStale(onMCPOffline, onHarnessOffline)
				h.pingAll()
			}
		}
	}()
}

func (h *Hub) pingAll() {
	h.mu.RLock()
	conns := make([]*Conn, 0, len(h.mcp)+len(h.harness))
	for _, c := range h.mcp {
		conns = append(conns, c)
	}
	for _, c := range h.harness {
		conns = append(conns, c)
	}
	h.mu.RUnlock()

	ping := mustJSON(map[string]string{"type": "ping"})
	for _, c := range conns {
		if c == nil || c.Send == nil {
			continue
		}
		if err := c.Send(ping); err != nil {
			log.Printf("relay: ping failed kind=%s id=%s: %v", c.Kind, connKey(c), err)
		}
	}
}

func (h *Hub) sweepStale(onMCPOffline, onHarnessOffline func(*Conn, string)) {
	now := time.Now()
	var staleMCP []struct {
		id   string
		conn *Conn
	}
	var staleHarness []struct {
		id   string
		conn *Conn
	}

	h.mu.Lock()
	for id, c := range h.mcp {
		if c != nil && now.Sub(c.LastSeen) > defaultStaleThreshold {
			staleMCP = append(staleMCP, struct {
				id   string
				conn *Conn
			}{id, c})
		}
	}
	for id, c := range h.harness {
		if c != nil && now.Sub(c.LastSeen) > defaultStaleThreshold {
			staleHarness = append(staleHarness, struct {
				id   string
				conn *Conn
			}{id, c})
		}
	}
	for _, item := range staleMCP {
		delete(h.mcp, item.id)
	}
	for _, item := range staleHarness {
		if c, ok := h.harness[item.id]; ok {
			if c.TenantID != "" && c.UserID != "" {
				delete(h.harnessByUser, c.TenantID+"#"+c.UserID)
			}
		}
		delete(h.harness, item.id)
	}
	h.mu.Unlock()

	for _, item := range staleMCP {
		log.Printf("relay: evicting stale MCP instance %s", item.id)
		if onMCPOffline != nil {
			onMCPOffline(item.conn, "heartbeat_timeout")
		}
	}
	for _, item := range staleHarness {
		log.Printf("relay: evicting stale harness worker %s", item.id)
		if onHarnessOffline != nil {
			onHarnessOffline(item.conn, "heartbeat_timeout")
		}
	}
}

func connKey(c *Conn) string {
	if c == nil {
		return ""
	}
	if c.InstanceID != "" {
		return c.InstanceID
	}
	return c.WorkerID
}
