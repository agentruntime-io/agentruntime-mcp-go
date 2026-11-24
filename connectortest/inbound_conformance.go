package connectortest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// ConformanceInternalToken is the internal token the fake control server expects.
const ConformanceInternalToken = "conformance-internal-token"

// ConformanceRouteToken is the route token the fake control server knows.
const ConformanceRouteToken = "conformance-route-token"

// FakeControl stands in for control-service in connector tests. It implements the two calls a
// webhook connector makes while receiving a vendor delivery:
//
//	POST /internal/trigger-ingress/resolve
//	POST /internal/trigger-events/connector
//
// Both require the internal token and the connector-router service name.
type FakeControl struct {
	t       *testing.T
	Server  *httptest.Server
	mu      sync.Mutex
	events  []map[string]any
	seen    map[string]bool
	ingress map[string]any

	// FailResolve and FailPost make the next call of that kind answer 500.
	FailResolve bool
	FailPost    bool
	// ResolveStatus and PostStatus, when non-zero, make that call answer this status with an error
	// body (401 and 403: control refuses the router; 429: control is overloaded).
	ResolveStatus int
	PostStatus    int
	// PostBody is the error body sent with PostStatus (for example {"error":"ingress_disabled"}).
	PostBody string
}

// NewFakeControl starts a fake control server. The ingress for ConformanceRouteToken uses
// the given vendor secret; extra routes (for example /v1/mcp/config/resolve-test) can be added.
func NewFakeControl(t *testing.T, ingress map[string]any, extra map[string]http.HandlerFunc) *FakeControl {
	t.Helper()
	fc := &FakeControl{t: t, seen: map[string]bool{}, ingress: ingress}
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/trigger-ingress/resolve", fc.resolve)
	mux.HandleFunc("/internal/trigger-events/connector", fc.post)
	for path, h := range extra {
		mux.HandleFunc(path, h)
	}
	fc.Server = httptest.NewServer(mux)
	t.Cleanup(fc.Server.Close)
	return fc
}

func (fc *FakeControl) authorized(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("X-Internal-Token") != ConformanceInternalToken || r.Header.Get("X-Service-Name") != "connector-router" {
		w.WriteHeader(http.StatusUnauthorized)
		return false
	}
	return true
}

func (fc *FakeControl) resolve(w http.ResponseWriter, r *http.Request) {
	if !fc.authorized(w, r) {
		return
	}
	fc.mu.Lock()
	fail := fc.FailResolve
	status := fc.ResolveStatus
	ingress := fc.ingress
	fc.mu.Unlock()
	if fail {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if status != 0 {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
		return
	}
	var req struct {
		RouteToken string `json:"route_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.RouteToken != ConformanceRouteToken || ingress == nil {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not_found"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ingress)
}

func (fc *FakeControl) post(w http.ResponseWriter, r *http.Request) {
	if !fc.authorized(w, r) {
		return
	}
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if fc.FailPost {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if fc.PostStatus != 0 {
		w.WriteHeader(fc.PostStatus)
		body := fc.PostBody
		if body == "" {
			body = `{"error":"refused"}`
		}
		_, _ = w.Write([]byte(body))
		return
	}
	var ev map[string]any
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	fc.events = append(fc.events, ev)
	key, _ := ev["event_key"].(string)
	dup := fc.seen[key]
	fc.seen[key] = true
	status := http.StatusAccepted
	if dup {
		status = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"event_id": "evt-" + key, "duplicate": dup, "matched": 1, "fires": []any{}})
}

// Events returns the events the connector posted, in order.
func (fc *FakeControl) Events() []map[string]any {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return append([]map[string]any(nil), fc.events...)
}

// InboundWebhookConformance is the suite every connector that receives vendor webhooks must pass.
// It checks the behaviours that keep a public route safe: no credential, a wrong credential and
// an empty secret are refused, an oversize body is refused, a replay carries the same event key
// (so the platform dedupes it), an unknown or disabled route is a 404, and a failing control
// service makes the vendor retry.
type InboundWebhookConformance struct {
	Adapter string
	// Handler serves the connector's webhook routes, typically a mux after RegisterWebhook.
	Handler http.Handler
	// VendorSecret is the secret a valid delivery is signed with: the ingress vendor secret the
	// fake control reports, unless IngressVendorSecret says the connector must not use that one.
	VendorSecret string
	// IngressVendorSecret is what the fake control reports as the ingress vendor secret when it
	// differs from VendorSecret: a connector that verifies with a credential held on the connection
	// (payments-lk's merchant key) must ignore it. Empty means VendorSecret.
	IngressVendorSecret string
	// Body is a valid vendor delivery body (a JSON object).
	Body        []byte
	ContentType string
	// Prepare adds the non-credential headers a real delivery carries (event name, delivery id).
	Prepare func(r *http.Request, body []byte)
	// Sign adds a valid vendor credential computed with secret. It is also called with a wrong
	// secret and with an empty secret, which must both be refused.
	Sign func(r *http.Request, body []byte, secret string)
	// WantEventType and WantEventKey are what the valid delivery must be posted as.
	WantEventType string
	WantEventKey  string
	// MaxBodyBytes is the body cap the connector declares (default 1 MiB).
	MaxBodyBytes int
	// ControlRoutes adds control endpoints the connector calls while verifying. emptySecret
	// asks for the connector's own secret source to be empty (payments-lk reads the merchant
	// key from the connection config).
	ControlRoutes func(emptySecret bool) map[string]http.HandlerFunc
	// SignStale, when set, adds a credential that is correct except that its timestamp is outside
	// the vendor's tolerance (Fathom, payments-lk): it must be refused like a wrong credential.
	SignStale func(r *http.Request, body []byte, secret string)
	// Ingress fields reported next to the vendor secret.
	TenantID, ProjectID, ServerID, ConnectionID string
}

func (c InboundWebhookConformance) ingress(secret string) map[string]any {
	str := func(v, def string) string {
		if v == "" {
			return def
		}
		return v
	}
	return map[string]any{
		"vendor_secret": secret,
		"connection_id": str(c.ConnectionID, "conn-1"),
		"enabled":       true,
		"tenant_id":     str(c.TenantID, "tenant-1"),
		"project_id":    str(c.ProjectID, "project-1"),
		// control stores the catalog row id (a UUID) as the ingress server id, not the adapter name
		"server_id": str(c.ServerID, "6b1f4c52-8f3e-4a52-9d0a-0d4b1b7a2c11"),
	}
}

func (c InboundWebhookConformance) request(token string, body []byte, sign func(r *http.Request, body []byte)) *http.Request {
	ct := c.ContentType
	if ct == "" {
		ct = "application/json"
	}
	r := httptest.NewRequest(http.MethodPost, "/"+c.Adapter+"/webhook/"+token, bytes.NewReader(body))
	r.Header.Set("Content-Type", ct)
	if c.Prepare != nil {
		c.Prepare(r, body)
	}
	if sign != nil {
		sign(r, body)
	}
	return r
}

// Run executes the suite as subtests.
func (c InboundWebhookConformance) Run(t *testing.T) {
	t.Helper()
	if c.Handler == nil || c.Sign == nil || c.VendorSecret == "" || len(c.Body) == 0 || c.WantEventType == "" || c.WantEventKey == "" {
		t.Fatal("conformance: Handler, Sign, VendorSecret, Body, WantEventType and WantEventKey are required")
	}
	max := c.MaxBodyBytes
	if max <= 0 {
		max = 1 << 20
	}

	// newControl starts a fake control server and points the router environment at it.
	newControl := func(t *testing.T, secret string, emptySecret bool) *FakeControl {
		t.Helper()
		var extra map[string]http.HandlerFunc
		if c.ControlRoutes != nil {
			extra = c.ControlRoutes(emptySecret)
		}
		ingressSecret := secret
		if secret != "" && c.IngressVendorSecret != "" {
			ingressSecret = c.IngressVendorSecret
		}
		fc := NewFakeControl(t, c.ingress(ingressSecret), extra)
		t.Setenv("MCP_CONTROL_SERVER_URL", fc.Server.URL)
		t.Setenv("MCP_CONTROL_INTERNAL_TOKEN", ConformanceInternalToken)
		return fc
	}
	valid := func(secret string) func(r *http.Request, body []byte) {
		return func(r *http.Request, body []byte) { c.Sign(r, body, secret) }
	}
	do := func(r *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		c.Handler.ServeHTTP(rec, r)
		return rec
	}
	wantStatus := func(t *testing.T, rec *httptest.ResponseRecorder, want int) {
		t.Helper()
		if rec.Code != want {
			b, _ := io.ReadAll(rec.Body)
			t.Fatalf("status = %d, want %d (body %q)", rec.Code, want, strings.TrimSpace(string(b)))
		}
	}
	wantNoEvents := func(t *testing.T, fc *FakeControl) {
		t.Helper()
		if n := len(fc.Events()); n != 0 {
			t.Fatalf("%d event(s) reached control, want none", n)
		}
	}

	t.Run("valid delivery is posted to control", func(t *testing.T) {
		fc := newControl(t, c.VendorSecret, false)
		rec := do(c.request(ConformanceRouteToken, c.Body, valid(c.VendorSecret)))
		wantStatus(t, rec, http.StatusOK)
		events := fc.Events()
		if len(events) != 1 {
			t.Fatalf("%d events posted, want 1", len(events))
		}
		ev := events[0]
		if ev["route_token"] != ConformanceRouteToken {
			t.Errorf("route_token = %v", ev["route_token"])
		}
		if ev["event_type"] != c.WantEventType {
			t.Errorf("event_type = %v, want %q", ev["event_type"], c.WantEventType)
		}
		if ev["event_key"] != c.WantEventKey {
			t.Errorf("event_key = %v, want %q", ev["event_key"], c.WantEventKey)
		}
		if _, hasRaw := ev["payload_raw"]; hasRaw {
			t.Errorf("a JSON object body must be sent as payload, not payload_raw")
		}
		var want any
		_ = json.Unmarshal(c.Body, &want)
		if !reflect.DeepEqual(ev["payload"], want) {
			t.Errorf("payload = %v, want the vendor body %v", ev["payload"], want)
		}
		if ev["channel"] != nil {
			t.Errorf("channel must be null for a connector event, got %v", ev["channel"])
		}
	})

	t.Run("no credential is refused", func(t *testing.T) {
		fc := newControl(t, c.VendorSecret, false)
		wantStatus(t, do(c.request(ConformanceRouteToken, c.Body, nil)), http.StatusUnauthorized)
		wantNoEvents(t, fc)
	})

	t.Run("wrong credential is refused", func(t *testing.T) {
		fc := newControl(t, c.VendorSecret, false)
		wantStatus(t, do(c.request(ConformanceRouteToken, c.Body, valid("not-"+c.VendorSecret))), http.StatusUnauthorized)
		wantNoEvents(t, fc)
	})

	t.Run("empty vendor secret is refused even with a matching empty credential", func(t *testing.T) {
		fc := newControl(t, "", true)
		wantStatus(t, do(c.request(ConformanceRouteToken, c.Body, valid(""))), http.StatusUnauthorized)
		wantStatus(t, do(c.request(ConformanceRouteToken, c.Body, nil)), http.StatusUnauthorized)
		wantNoEvents(t, fc)
	})

	t.Run("oversize body is refused", func(t *testing.T) {
		fc := newControl(t, c.VendorSecret, false)
		big := []byte(`{"pad":"` + strings.Repeat("a", max) + `"}`)
		wantStatus(t, do(c.request(ConformanceRouteToken, big, valid(c.VendorSecret))), http.StatusRequestEntityTooLarge)
		wantNoEvents(t, fc)
	})

	t.Run("replay carries the same event key", func(t *testing.T) {
		fc := newControl(t, c.VendorSecret, false)
		wantStatus(t, do(c.request(ConformanceRouteToken, c.Body, valid(c.VendorSecret))), http.StatusOK)
		wantStatus(t, do(c.request(ConformanceRouteToken, c.Body, valid(c.VendorSecret))), http.StatusOK)
		events := fc.Events()
		if len(events) != 2 {
			t.Fatalf("%d events posted, want 2 (the platform, not the connector, drops the duplicate)", len(events))
		}
		if events[0]["event_key"] != events[1]["event_key"] || events[0]["event_key"] != c.WantEventKey {
			t.Fatalf("a replay must reuse the vendor's event key: %v then %v", events[0]["event_key"], events[1]["event_key"])
		}
	})

	t.Run("unknown route token is a 404", func(t *testing.T) {
		fc := newControl(t, c.VendorSecret, false)
		wantStatus(t, do(c.request("unknown-token", c.Body, valid(c.VendorSecret))), http.StatusNotFound)
		wantNoEvents(t, fc)
	})

	t.Run("disabled ingress is a 404", func(t *testing.T) {
		fc := newControl(t, c.VendorSecret, false)
		fc.mu.Lock()
		fc.ingress["enabled"] = false
		fc.mu.Unlock()
		wantStatus(t, do(c.request(ConformanceRouteToken, c.Body, valid(c.VendorSecret))), http.StatusNotFound)
		wantNoEvents(t, fc)
	})

	t.Run("other methods are refused", func(t *testing.T) {
		newControl(t, c.VendorSecret, false)
		r := httptest.NewRequest(http.MethodGet, "/"+c.Adapter+"/webhook/"+ConformanceRouteToken, nil)
		wantStatus(t, do(r), http.StatusMethodNotAllowed)
	})

	t.Run("a credential with a stale timestamp is refused", func(t *testing.T) {
		if c.SignStale == nil {
			t.Skip("the vendor's scheme has no timestamp")
		}
		fc := newControl(t, c.VendorSecret, false)
		wantStatus(t, do(c.request(ConformanceRouteToken, c.Body, func(r *http.Request, body []byte) { c.SignStale(r, body, c.VendorSecret) })), http.StatusUnauthorized)
		wantNoEvents(t, fc)
	})

	t.Run("oversize body without a content length is refused", func(t *testing.T) {
		fc := newControl(t, c.VendorSecret, false)
		big := []byte(`{"pad":"` + strings.Repeat("a", max) + `"}`)
		r := c.request(ConformanceRouteToken, big, valid(c.VendorSecret))
		r.ContentLength = -1 // chunked: the cap cannot rely on the header
		r.Body = io.NopCloser(bytes.NewReader(big))
		wantStatus(t, do(r), http.StatusRequestEntityTooLarge)
		wantNoEvents(t, fc)
	})

	t.Run("a body that is not JSON is never posted as an object", func(t *testing.T) {
		fc := newControl(t, c.VendorSecret, false)
		raw := []byte("event=ping&data=not-json")
		rec := do(c.request(ConformanceRouteToken, raw, valid(c.VendorSecret)))
		events := fc.Events()
		switch {
		case rec.Code == http.StatusBadRequest && len(events) == 0:
			// refused: the connector cannot name an event for it
		case rec.Code == http.StatusOK && len(events) == 1:
			if _, asObject := events[0]["payload"]; asObject {
				t.Fatalf("a non-JSON body must not become a payload object: %v", events[0])
			}
			if events[0]["payload_raw"] != string(raw) {
				t.Fatalf("payload_raw = %v, want the body verbatim", events[0]["payload_raw"])
			}
		default:
			t.Fatalf("status %d with %d events: want 400 and nothing posted, or 200 and one payload_raw event", rec.Code, len(events))
		}
	})

	t.Run("control refusing the router on lookup is retryable, never a silent 404", func(t *testing.T) {
		for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
			fc := newControl(t, c.VendorSecret, false)
			fc.ResolveStatus = status
			wantStatus(t, do(c.request(ConformanceRouteToken, c.Body, valid(c.VendorSecret))), http.StatusServiceUnavailable)
			wantNoEvents(t, fc)
		}
	})

	t.Run("control refusing or throttling the event is retryable", func(t *testing.T) {
		for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests} {
			fc := newControl(t, c.VendorSecret, false)
			fc.PostStatus = status
			wantStatus(t, do(c.request(ConformanceRouteToken, c.Body, valid(c.VendorSecret))), http.StatusBadGateway)
		}
	})

	t.Run("control saying the ingress was switched off meanwhile is a 404", func(t *testing.T) {
		fc := newControl(t, c.VendorSecret, false)
		fc.PostStatus = http.StatusForbidden
		fc.PostBody = `{"error":"ingress_disabled"}`
		wantStatus(t, do(c.request(ConformanceRouteToken, c.Body, valid(c.VendorSecret))), http.StatusNotFound)
	})

	t.Run("control rejecting the event as invalid is permanent", func(t *testing.T) {
		for _, status := range []int{http.StatusBadRequest, http.StatusUnprocessableEntity} {
			fc := newControl(t, c.VendorSecret, false)
			fc.PostStatus = status
			wantStatus(t, do(c.request(ConformanceRouteToken, c.Body, valid(c.VendorSecret))), http.StatusBadRequest)
		}
	})

	t.Run("control down on lookup makes the vendor retry", func(t *testing.T) {
		fc := newControl(t, c.VendorSecret, false)
		fc.FailResolve = true
		wantStatus(t, do(c.request(ConformanceRouteToken, c.Body, valid(c.VendorSecret))), http.StatusServiceUnavailable)
		wantNoEvents(t, fc)
	})

	t.Run("control down on post makes the vendor retry", func(t *testing.T) {
		fc := newControl(t, c.VendorSecret, false)
		fc.FailPost = true
		wantStatus(t, do(c.request(ConformanceRouteToken, c.Body, valid(c.VendorSecret))), http.StatusBadGateway)
	})
}
