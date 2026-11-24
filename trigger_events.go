package agentruntimemcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// TriggerEnv is how the connector router reaches control-service for trigger traffic:
// resolving an ingress route token and posting events. Both values come from the router
// environment (MCP_CONTROL_SERVER_URL, MCP_CONTROL_INTERNAL_TOKEN).
type TriggerEnv struct {
	ControlURL    string
	InternalToken string
	// HTTPClient is optional; a client with a 15 s timeout is used when nil.
	HTTPClient *http.Client
}

// LoadTriggerEnv reads the router environment.
func LoadTriggerEnv() TriggerEnv {
	return TriggerEnv{
		ControlURL:    strings.TrimRight(strings.TrimSpace(os.Getenv("MCP_CONTROL_SERVER_URL")), "/"),
		InternalToken: strings.TrimSpace(os.Getenv("MCP_CONTROL_INTERNAL_TOKEN")),
	}
}

func (e TriggerEnv) configured() bool {
	return strings.TrimSpace(e.ControlURL) != "" && strings.TrimSpace(e.InternalToken) != ""
}

func (e TriggerEnv) client() *http.Client {
	if e.HTTPClient != nil {
		return e.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (e TriggerEnv) url(path string) string {
	return strings.TrimRight(strings.TrimSpace(e.ControlURL), "/") + path
}

// TriggerEvent is what a connector hands to control for one vendor delivery
// (POST /internal/trigger-events/connector, contract section 5.1).
type TriggerEvent struct {
	RouteToken  string `json:"route_token"`
	EventType   string `json:"event_type"`
	EventKey    string `json:"event_key"`
	ContentType string `json:"content_type,omitempty"`
	// Payload is the vendor body when it is a JSON object, byte for byte.
	Payload json.RawMessage `json:"payload,omitempty"`
	// PayloadRaw carries the body when it is not a JSON object.
	PayloadRaw string `json:"payload_raw,omitempty"`
	// Channel is the channel envelope for sources that declare one; null for connectors.
	Channel json.RawMessage `json:"channel"`
}

// NewTriggerEvent builds the event for a vendor body: a JSON object goes to Payload
// untouched, anything else (array, scalar, form data, empty) to PayloadRaw.
func NewTriggerEvent(routeToken, eventType, eventKey, contentType string, body []byte) TriggerEvent {
	ev := TriggerEvent{
		RouteToken:  strings.TrimSpace(routeToken),
		EventType:   strings.TrimSpace(eventType),
		EventKey:    strings.TrimSpace(eventKey),
		ContentType: strings.TrimSpace(contentType),
	}
	trimmed := bytes.TrimSpace(body)
	var probe map[string]json.RawMessage
	if len(trimmed) > 0 && trimmed[0] == '{' && json.Unmarshal(trimmed, &probe) == nil {
		ev.Payload = json.RawMessage(append([]byte(nil), trimmed...))
	} else {
		ev.PayloadRaw = string(body)
	}
	return ev
}

// TriggerAck is control's answer to a posted event.
type TriggerAck struct {
	EventID   string           `json:"event_id"`
	Duplicate bool             `json:"duplicate"`
	Matched   int              `json:"matched"`
	Fires     []TriggerAckFire `json:"fires"`
}

// TriggerAckFire names one fire created for the event.
type TriggerAckFire struct {
	TriggerID string `json:"trigger_id"`
	FireID    string `json:"fire_id"`
}

// TriggerPostError is a non-2xx answer from control.
type TriggerPostError struct {
	Status int
	Body   string
}

func (e *TriggerPostError) Error() string {
	return fmt.Sprintf("control trigger event: status %d: %s", e.Status, e.Body)
}

// PostTriggerEvent posts one vendor event to control. Control answers 202 after the event
// and its fires are committed, before any run starts, so a vendor is never kept waiting on a
// run. A repeat of the same (route, event_key) is acknowledged with duplicate=true.
// A non-2xx answer is a *TriggerPostError; a transport failure is returned as is.
func PostTriggerEvent(ctx context.Context, env TriggerEnv, ev TriggerEvent) (*TriggerAck, error) {
	if !env.configured() {
		return nil, fmt.Errorf("MCP_CONTROL_SERVER_URL and MCP_CONTROL_INTERNAL_TOKEN required to post trigger events")
	}
	if ev.RouteToken == "" || ev.EventType == "" || ev.EventKey == "" {
		return nil, fmt.Errorf("trigger event needs route_token, event_type and event_key")
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, env.url("/internal/trigger-events/connector"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set(inboundInternalTokenHeader, env.InternalToken)
	req.Header.Set("X-Service-Name", ServiceConnectorRouter)
	resp, err := env.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &TriggerPostError{Status: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	var ack TriggerAck
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &ack); err != nil {
			return nil, fmt.Errorf("control trigger event: invalid response: %w", err)
		}
	}
	return &ack, nil
}

// TriggerIngress is what control knows about a route token.
type TriggerIngress struct {
	VendorSecret string `json:"vendor_secret"`
	ConnectionID string `json:"connection_id"`
	Enabled      bool   `json:"enabled"`
	// TenantID, ProjectID and ServerID identify the connection, for connectors that verify
	// with a credential held on the connection (payments-lk) instead of the vendor secret.
	TenantID  string `json:"tenant_id"`
	ProjectID string `json:"project_id"`
	ServerID  string `json:"server_id"`
}

// ResolveTriggerIngress asks control which ingress a route token belongs to
// (POST /internal/trigger-ingress/resolve). It returns (nil, nil) when the token is unknown (404)
// or the ingress is off, and an error for any other answer, so a vendor retries.
func ResolveTriggerIngress(ctx context.Context, env TriggerEnv, routeToken string) (*TriggerIngress, error) {
	routeToken = strings.TrimSpace(routeToken)
	if routeToken == "" {
		return nil, fmt.Errorf("route_token required")
	}
	if !env.configured() {
		return nil, fmt.Errorf("MCP_CONTROL_SERVER_URL and MCP_CONTROL_INTERNAL_TOKEN required for trigger ingress lookup")
	}
	body, err := json.Marshal(map[string]string{"route_token": routeToken})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, env.url("/internal/trigger-ingress/resolve"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set(inboundInternalTokenHeader, env.InternalToken)
	req.Header.Set("X-Service-Name", ServiceConnectorRouter)
	resp, err := env.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	// Only 404 means "no such route". 401 and 403 mean control does not accept this router (a wrong
	// token or service name in one environment), and 429 or 5xx mean it is busy: all are errors, so the
	// vendor retries and the router logs why, instead of every delivery turning into a silent 404.
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("control trigger ingress lookup: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out TriggerIngress
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("control trigger ingress lookup: invalid response: %w", err)
	}
	if !out.Enabled {
		return nil, nil
	}
	return &out, nil
}
