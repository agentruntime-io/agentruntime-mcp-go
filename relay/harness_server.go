package relay

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// HarnessWorkersPath lists the harness workers of a tenant and the agents they advertised.
const HarnessWorkersPath = "/relay/harness/workers"

// maxHarnessJobBytes bounds a dispatched job body (prompt, config, headers).
const maxHarnessJobBytes = 8 << 20

func (s *Server) handleHarnessConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, status, err := s.authenticateConnect(r)
	if err != nil {
		http.Error(w, err.Error(), status)
		return
	}
	workerID := strings.TrimSpace(r.URL.Query().Get("worker_id"))
	if err := checkWorkerID(workerID, id); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if workerID == "" {
		workerID = uuid.NewString()
	}
	tenantID := id.TenantID
	userID := id.UserID
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	tier := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("tier")))

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	conn := &Conn{
		ID:       uuid.NewString(),
		WorkerID: workerID,
		TenantID: tenantID,
		UserID:   userID,
		Provider: provider,
		Tier:     tier,
		Send: func(b []byte) error {
			return ws.WriteMessage(websocket.TextMessage, b)
		},
	}
	s.Hub.RegisterHarness(workerID, conn)
	if s.Config.OrchestratorCallback != nil {
		cb := s.Config.OrchestratorCallback
		go cb.NotifyWorkerOnline(context.Background(), workerID, tenantID, userID)
	}
	defer func() {
		s.Hub.UnregisterHarness(conn)
		_ = ws.Close()
	}()

	log.Printf("relay: harness worker %s connected (tenant=%s user=%s tier=%s verified=%t)", workerID, tenantID, userID, tier, id.Verified)
	_ = conn.Send(mustJSON(map[string]any{"type": "harness_registered", "worker_id": workerID}))

	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			return
		}
		s.Hub.Touch(conn)
		s.dispatchHarnessInbound(conn, msg)
	}
}

func (s *Server) dispatchHarnessInbound(conn *Conn, msg []byte) {
	var frame struct {
		Type         string         `json:"type"`
		RunID        string         `json:"run_id"`
		Status       string         `json:"status"`
		Result       map[string]any `json:"result"`
		Error        string         `json:"error"`
		WorkerID     string         `json:"worker_id"`
		Capabilities struct {
			Tier          string   `json:"tier"`
			Providers     []string `json:"providers"`
			MaxConcurrent int      `json:"max_concurrent"`
			// Setups is the agents the worker offers. It stays raw so a malformed
			// list cannot cost the frame its tier and providers.
			Setups json.RawMessage `json:"setups"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(msg, &frame); err != nil {
		return
	}
	switch frame.Type {
	case "pong", "heartbeat":
		return
	case "harness_register":
		// The worker id is fixed at connect, where it was authorized. A register
		// frame naming another id would re-key the worker, so it is ignored.
		if frame.WorkerID != "" && frame.WorkerID != conn.WorkerID {
			log.Printf("relay: harness worker %s sent harness_register for %q; keeping the connect-time id", conn.WorkerID, frame.WorkerID)
		}
		s.Hub.UpdateHarnessCapabilities(conn, frame.Capabilities.Tier, frame.Capabilities.Providers, frame.Capabilities.MaxConcurrent)
		s.Hub.SetHarnessAgents(conn, parseHarnessSetups(frame.Capabilities.Setups))
	case "harness_complete":
		if !s.Hub.ConsumeDispatch(conn, frame.RunID) {
			log.Printf("relay: dropping harness_complete for run %q from worker %s (tenant=%s): not dispatched to this worker", frame.RunID, conn.WorkerID, conn.TenantID)
			return
		}
		s.Hub.IncHarnessActiveJobs(conn, -1)
		log.Printf("relay: harness run %s completed status=%s", frame.RunID, frame.Status)
		if s.Config.OrchestratorCallback != nil {
			cb := s.Config.OrchestratorCallback
			go cb.NotifyRunComplete(context.Background(), conn.TenantID, conn.WorkerID, frame.RunID, frame.Status, frame.Result, frame.Error)
		}
	}
}

func (s *Server) handleHarnessPick(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if code, msg := s.authorizeInternal(r); code != 0 {
		http.Error(w, msg, code)
		return
	}
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if tenantID == "" && !s.Config.AllowLegacyDispatch && !s.Config.AllowInsecure {
		http.Error(w, "tenant_id required", http.StatusBadRequest)
		return
	}
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	tier := strings.TrimSpace(r.URL.Query().Get("tier"))
	conn, ok := s.Hub.PickSharedWorker(tenantID, provider, tier)
	if !ok {
		http.Error(w, "no shared worker available", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"worker_id":      conn.WorkerID,
		"tier":           conn.Tier,
		"active_jobs":    conn.ActiveJobs,
		"max_concurrent": conn.MaxConcurrent,
	})
}

func (s *Server) handleHarnessRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if code, msg := s.authorizeInternal(r); code != 0 {
		http.Error(w, msg, code)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, HarnessWorkerPrefix)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "worker_id required", http.StatusBadRequest)
		return
	}
	workerID := parts[0]
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))

	var (
		conn *Conn
		ok   bool
	)
	switch {
	case tenantID != "":
		conn, ok = s.Hub.GetHarness(tenantID, workerID)
	case s.Config.AllowLegacyDispatch || s.Config.AllowInsecure:
		conn, ok = s.Hub.GetHarnessAnyTenant(workerID)
	default:
		http.Error(w, "X-Tenant-Id required", http.StatusBadRequest)
		return
	}
	if !ok {
		// Also what a worker bound to another tenant looks like: it is not this tenant's.
		http.Error(w, "harness worker not connected", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxHarnessJobBytes))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	var job map[string]any
	if err := json.Unmarshal(body, &job); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	runID, _ := job["run_id"].(string)
	if runID == "" {
		runID = uuid.NewString()
		job["run_id"] = runID
	}
	job["type"] = "harness_run"
	job["id"] = uuid.NewString()
	s.Hub.TrackDispatch(conn, runID)
	s.Hub.IncHarnessActiveJobs(conn, 1)
	if err := conn.Send(mustJSON(job)); err != nil {
		s.Hub.IncHarnessActiveJobs(conn, -1)
		s.Hub.ForgetDispatch(conn, runID)
		http.Error(w, "forward failed", http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "dispatched", "run_id": runID})
}

// handleHarnessWorkers lists a tenant's harness workers with the agents each
// advertised, online or last seen. AgentRuntime calls it with X-Internal-Token;
// tenant_id is required, and user_id leaves out the workers of other users.
func (s *Server) handleHarnessWorkers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if code, msg := s.authorizeInternalStrict(r); code != 0 {
		http.Error(w, msg, code)
		return
	}
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if tenantID == "" {
		http.Error(w, "tenant_id required", http.StatusBadRequest)
		return
	}
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	writeJSON(w, http.StatusOK, map[string]any{"workers": s.Hub.ListHarnessWorkers(tenantID, userID)})
}
