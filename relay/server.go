package relay



import (

	"context"

	"encoding/json"

	"io"

	"log"

	"net/http"

	"os"

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

	// AuthToken is a static bearer accepted on WSS connect (RELAY_AUTH_TOKEN, dev).
	// The tenant and user of such a connection are whatever the client claims.

	AuthToken            string

	// Verifier validates PAT / Keycloak JWT bearers on WSS connect and supplies the
	// tenant and user the connection is bound to. Nil unless configured.

	Verifier             TokenVerifier

	// InternalToken is the X-Internal-Token AgentRuntime must send to dispatch a
	// harness job or pick a shared worker (MCP_CONTROL_INTERNAL_TOKEN).

	InternalToken        string

	// AllowInsecure turns authentication off (RELAY_ALLOW_INSECURE, local dev only).

	AllowInsecure        bool

	// AllowLegacyDispatch accepts dispatch/pick requests that carry no
	// X-Internal-Token or X-Tenant-Id (RELAY_ALLOW_LEGACY_DISPATCH), for the rollout
	// window while AgentRuntime still sends the old shape. Remove it afterwards.

	AllowLegacyDispatch  bool

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

	s.logAuthMode()

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

	id, status, err := s.authenticateConnect(r)

	if err != nil {

		http.Error(w, err.Error(), status)

		return

	}

	instanceID := strings.TrimSpace(r.URL.Query().Get("instance_id"))

	if instanceID == "" {

		http.Error(w, "instance_id required", http.StatusBadRequest)

		return

	}

	tenantID := id.TenantID

	userID := id.UserID

	if s.Hub.MCPHeldByOtherTenant(instanceID, tenantID) {

		http.Error(w, "instance_id is connected for another tenant", http.StatusConflict)

		return

	}



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

	if !s.Hub.TryRegisterMCP(instanceID, conn) {

		_ = ws.Close()

		return

	}

	if s.Config.ControlCallback != nil {

		if url := s.instanceCanonicalURL(instanceID); url != "" {

			cb := s.Config.ControlCallback

			go cb.NotifyInstanceConnected(context.Background(), instanceID, tenantID, url)

		}

	}

	defer func() {

		s.Hub.UnregisterMCPConn(instanceID, conn)

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

		return strings.TrimRight(strings.TrimSpace(os.Getenv("RELAY_PUBLIC_BASE_URL")), "/")

	}

	return strings.TrimRight(strings.TrimSpace(getenv("RELAY_PUBLIC_BASE_URL")), "/")

}


