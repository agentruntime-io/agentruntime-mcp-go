package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	MCPConnectPath       = "/v1/connect"
	HarnessConnectPath   = "/v1/harness/connect"
	MCPInstancePrefix    = "/relay/mcp/instances/"
	HarnessWorkerPrefix  = "/relay/harness/workers/"
	HarnessPickPath      = "/relay/harness/pick"
	defaultMCPForwardTO  = 120 // seconds
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type ServerConfig struct {
	AuthToken            string
	PublicBaseURL        string // https://relay.example.com — used for canonical_url callbacks
	OrchestratorCallback *OrchestratorCallback
	ControlCallback      *ControlCallback
}

type Server struct {
	Hub    *Hub
	Config ServerConfig

	heartbeatOnce sync.Once
}

func NewServer(cfg ServerConfig) *Server {
	h := DefaultHub
	if h == nil {
		h = NewHub()
	}
	return &Server{Hub: h, Config: cfg}
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	s.startHeartbeat()
	mux.HandleFunc(MCPConnectPath, s.handleMCPConnect)
	mux.HandleFunc(HarnessConnectPath, s.handleHarnessConnect)
	mux.HandleFunc(MCPInstancePrefix, s.handleMCPForward)
	mux.HandleFunc(HarnessWorkerPrefix, s.handleHarnessRun)
	mux.HandleFunc(HarnessPickPath, s.handleHarnessPick)
}

func (s *Server) startHeartbeat() {
	s.heartbeatOnce.Do(func() {
		s.Hub.StartHeartbeatMonitor(context.Background(), s.onMCPOffline, s.onHarnessOffline)
	})
}

func (s *Server) onMCPOffline(c *Conn, reason string) {
	if c == nil || s.Config.ControlCallback == nil {
		return
	}
	cb := s.Config.ControlCallback
	go cb.NotifyInstanceDisconnected(context.Background(), c.InstanceID, c.TenantID, reason)
}

func (s *Server) onHarnessOffline(c *Conn, reason string) {
	if c == nil {
		return
	}
	log.Printf("relay: harness worker %s offline (%s)", c.WorkerID, reason)
}

func (s *Server) authorizeConnect(r *http.Request) error {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		return errors.New("missing authorization")
	}
	if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return errors.New("authorization must be bearer token")
	}
	token := strings.TrimSpace(auth[7:])
	if token == "" {
		return errors.New("empty bearer token")
	}
	if s.Config.AuthToken != "" && token != s.Config.AuthToken {
		return errors.New("invalid bearer token")
	}
	return nil
}

func (s *Server) instanceCanonicalURL(instanceID string) string {
	base := strings.TrimRight(strings.TrimSpace(s.Config.PublicBaseURL), "/")
	if base == "" {
		return ""
	}
	return base + MCPInstancePrefix + instanceID
}

func (s *Server) handleMCPConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.authorizeConnect(r); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	instanceID := strings.TrimSpace(r.URL.Query().Get("instance_id"))
	if instanceID == "" {
		http.Error(w, "instance_id required", http.StatusBadRequest)
		return
	}
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	conn := &Conn{
		ID:         uuid.NewString(),
		InstanceID: instanceID,
		TenantID:   tenantID,
		UserID:     userID,
		Mux:        NewRequestMultiplexer(0),
		Send: func(b []byte) error {
			return ws.WriteMessage(websocket.TextMessage, b)
		},
	}
	s.Hub.RegisterMCP(instanceID, conn)
	if s.Config.ControlCallback != nil {
		if url := s.instanceCanonicalURL(instanceID); url != "" {
			cb := s.Config.ControlCallback
			go cb.NotifyInstanceConnected(context.Background(), instanceID, tenantID, url)
		}
	}
	defer func() {
		s.Hub.UnregisterMCP(instanceID)
		if s.Config.ControlCallback != nil {
			cb := s.Config.ControlCallback
			go cb.NotifyInstanceDisconnected(context.Background(), instanceID, tenantID, "disconnect")
		}
		_ = ws.Close()
	}()

	log.Printf("relay: MCP instance %s connected (tenant=%s)", instanceID, tenantID)
	_ = conn.Send(mustJSON(map[string]any{
		"type":          "mcp_connected",
		"instance_id":   instanceID,
		"canonical_url": s.instanceCanonicalURL(instanceID),
	}))

	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			return
		}
		s.Hub.Touch(conn)
		s.dispatchMCPInbound(conn, msg)
	}
}

func (s *Server) dispatchMCPInbound(conn *Conn, msg []byte) {
	var frame struct {
		Type   string          `json:"type"`
		ID     string          `json:"id"`
		Body   json.RawMessage `json:"body"`
		Status int             `json:"status"`
	}
	if err := json.Unmarshal(msg, &frame); err != nil {
		return
	}
	switch frame.Type {
	case "pong", "heartbeat":
		return
	case "mcp_response":
		if conn != nil && conn.Mux != nil && frame.ID != "" {
			conn.Mux.Complete(frame.ID, frame.Body)
		}
	}
}

func (s *Server) handleHarnessConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.authorizeConnect(r); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	workerID := strings.TrimSpace(r.URL.Query().Get("worker_id"))
	if workerID == "" {
		workerID = uuid.NewString()
	}
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
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
	if tier != "" {
		conn.Tier = tier
	}
	s.Hub.RegisterHarness(workerID, conn)
	if s.Config.OrchestratorCallback != nil {
		cb := s.Config.OrchestratorCallback
		go cb.NotifyWorkerOnline(context.Background(), workerID, tenantID, userID)
	}
	defer func() {
		s.Hub.UnregisterHarness(workerID)
		_ = ws.Close()
	}()

	log.Printf("relay: harness worker %s connected (tenant=%s user=%s tier=%s)", workerID, tenantID, userID, tier)
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
		Type         string   `json:"type"`
		RunID        string   `json:"run_id"`
		Status       string   `json:"status"`
		Result       map[string]any `json:"result"`
		Error        string   `json:"error"`
		WorkerID     string   `json:"worker_id"`
		Capabilities struct {
			Tier          string   `json:"tier"`
			Providers     []string `json:"providers"`
			MaxConcurrent int      `json:"max_concurrent"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(msg, &frame); err != nil {
		return
	}
	switch frame.Type {
	case "pong", "heartbeat":
		return
	case "harness_register":
		workerID := conn.WorkerID
		if frame.WorkerID != "" {
			workerID = frame.WorkerID
		}
		s.Hub.UpdateHarnessCapabilities(workerID, frame.Capabilities.Tier, frame.Capabilities.Providers, frame.Capabilities.MaxConcurrent)
	case "harness_complete":
		s.Hub.IncHarnessActiveJobs(conn.WorkerID, -1)
		log.Printf("relay: harness run %s completed status=%s", frame.RunID, frame.Status)
		if s.Config.OrchestratorCallback != nil {
			cb := s.Config.OrchestratorCallback
			go cb.NotifyRunComplete(context.Background(), frame.RunID, frame.Status, frame.Result, frame.Error)
		}
	}
}

func (s *Server) handleMCPForward(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, MCPInstancePrefix)
	instanceID := strings.Trim(strings.Split(path, "/")[0], "/")
	if instanceID == "" {
		http.Error(w, "instance_id required", http.StatusBadRequest)
		return
	}
	conn, ok := s.Hub.GetMCP(instanceID)
	if !ok {
		http.Error(w, "connector not connected", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	reqID := uuid.NewString()
	if conn.Mux == nil {
		conn.Mux = NewRequestMultiplexer(0)
	}
	respCh := conn.Mux.Register(reqID)
	frame := map[string]any{
		"type": "mcp_request",
		"id":   reqID,
		"body": json.RawMessage(body),
		"headers": map[string]string{
			"authorization":     r.Header.Get("Authorization"),
			"x-mcp-instance-id": instanceID,
		},
	}
	if err := conn.Send(mustJSON(frame)); err != nil {
		conn.Mux.Cancel(reqID)
		http.Error(w, "forward failed", http.StatusBadGateway)
		return
	}

	if wait := strings.TrimSpace(r.Header.Get("X-Relay-Async")); wait == "1" || strings.EqualFold(wait, "true") {
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "forwarded", "id": reqID})
		return
	}

	respBody, ok := conn.Mux.Wait(respCh)
	if !ok {
		conn.Mux.Cancel(reqID)
		http.Error(w, "relay response timeout", http.StatusGatewayTimeout)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respBody)
}

func (s *Server) handleHarnessPick(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
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
	path := strings.TrimPrefix(r.URL.Path, HarnessWorkerPrefix)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "worker_id required", http.StatusBadRequest)
		return
	}
	workerID := parts[0]
	conn, ok := s.Hub.GetHarness(workerID)
	if !ok {
		http.Error(w, "harness worker not connected", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(r.Body)
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
	s.Hub.IncHarnessActiveJobs(workerID, 1)
	if err := conn.Send(mustJSON(job)); err != nil {
		s.Hub.IncHarnessActiveJobs(workerID, -1)
		http.Error(w, "forward failed", http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "dispatched", "run_id": runID})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func AuthTokenFromEnv(getenv func(string) string) string {
	if getenv == nil {
		return ""
	}
	return strings.TrimSpace(getenv("RELAY_AUTH_TOKEN"))
}

func PublicBaseURLFromEnv(getenv func(string) string) string {
	if getenv == nil {
		return ""
	}
	if v := strings.TrimSpace(getenv("RELAY_PUBLIC_BASE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(getenv("HARNESS_RELAY_URL")); v != "" {
		v = strings.TrimPrefix(v, "wss://")
		v = strings.TrimPrefix(v, "ws://")
		v = strings.TrimPrefix(v, "https://")
		v = strings.TrimPrefix(v, "http://")
		if i := strings.Index(v, "/"); i > 0 {
			v = v[:i]
		}
		return "https://" + v
	}
	return ""
}
