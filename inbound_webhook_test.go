package agentruntimemcp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/agentruntime-io/agentruntime-mcp-go/connectortest"
)

const testSigHeader = "X-Test-Signature"

func testSign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func testVerify(r *http.Request, body []byte, secret string) error {
	got := r.Header.Get(testSigHeader)
	if got == "" {
		return fmt.Errorf("missing signature")
	}
	if !ConstantTimeEqual(got, testSign(secret, body)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

func testSpec() InboundWebhookSpec {
	return InboundWebhookSpec{
		Adapter: "acme",
		Verify:  testVerify,
		EventKey: func(r *http.Request, body []byte) string {
			if id := r.Header.Get("X-Delivery"); id != "" {
				return "acme-" + id
			}
			return ""
		},
		EventType: func(r *http.Request, body []byte) string { return r.Header.Get("X-Event") },
	}
}

func testMux(spec InboundWebhookSpec) http.Handler {
	mux := http.NewServeMux()
	MountInboundWebhook(mux, spec)
	return mux
}

func TestServeInboundWebhook_conformance(t *testing.T) {
	connectortest.InboundWebhookConformance{
		Adapter:      "acme",
		Handler:      testMux(testSpec()),
		VendorSecret: "whsec-acme",
		Body:         []byte(`{"id":7,"kind":"thing"}`),
		Prepare: func(r *http.Request, body []byte) {
			r.Header.Set("X-Event", "thing.created")
			r.Header.Set("X-Delivery", "d-1")
		},
		Sign: func(r *http.Request, body []byte, secret string) {
			r.Header.Set(testSigHeader, testSign(secret, body))
		},
		WantEventType: "thing.created",
		WantEventKey:  "acme-d-1",
	}.Run(t)
}

func TestServeInboundWebhook_secretHookOverridesVendorSecret(t *testing.T) {
	spec := testSpec()
	var seenSecret atomic.Value
	spec.Verify = func(r *http.Request, body []byte, secret string) error {
		seenSecret.Store(secret)
		return testVerify(r, body, secret)
	}
	spec.Secret = func(ctx context.Context, ing TriggerIngress) (string, error) {
		if ing.TenantID != "tenant-1" || ing.ServerID == "" || ing.ConnectionID != "conn-1" {
			return "", fmt.Errorf("ingress context missing: %+v", ing)
		}
		return "merchant-key", nil
	}
	connectortest.InboundWebhookConformance{
		Adapter:             "acme",
		Handler:             testMux(spec),
		VendorSecret:        "merchant-key",
		IngressVendorSecret: "placeholder-the-connector-must-ignore",
		Body:                []byte(`{"id":7}`),
		Prepare: func(r *http.Request, body []byte) {
			r.Header.Set("X-Event", "thing.created")
			r.Header.Set("X-Delivery", "d-1")
		},
		Sign: func(r *http.Request, body []byte, secret string) {
			r.Header.Set(testSigHeader, testSign(secret, body))
		},
		WantEventType: "thing.created",
		WantEventKey:  "acme-d-1",
	}.Run(t)
	if got, _ := seenSecret.Load().(string); got != "merchant-key" {
		t.Fatalf("Verify saw %q, want the secret from the Secret hook", got)
	}
}

func TestServeInboundWebhook_eventKeyFallsBackToBodyDigest(t *testing.T) {
	fc := connectortest.NewFakeControl(t, map[string]any{"vendor_secret": "s", "connection_id": "c", "enabled": true}, nil)
	t.Setenv("MCP_CONTROL_SERVER_URL", fc.Server.URL)
	t.Setenv("MCP_CONTROL_INTERNAL_TOKEN", connectortest.ConformanceInternalToken)
	spec := testSpec()
	spec.EventKey = func(*http.Request, []byte) string { return "" }
	h := testMux(spec)

	body := []byte(`{"a":1}`)
	post := func(b []byte) {
		r := httptest.NewRequest(http.MethodPost, "/acme/webhook/"+connectortest.ConformanceRouteToken, strings.NewReader(string(b)))
		r.Header.Set("X-Event", "e")
		r.Header.Set(testSigHeader, testSign("s", b))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != 200 {
			t.Fatalf("status %d", rec.Code)
		}
	}
	post(body)
	post(body)
	post([]byte(`{"a":2}`))
	ev := fc.Events()
	if len(ev) != 3 {
		t.Fatalf("%d events", len(ev))
	}
	k0, k1, k2 := ev[0]["event_key"].(string), ev[1]["event_key"].(string), ev[2]["event_key"].(string)
	if !strings.HasPrefix(k0, "acme-sha256-") || k0 != k1 || k0 == k2 {
		t.Fatalf("fallback keys: %q %q %q (same body must give the same key, a different body a different one)", k0, k1, k2)
	}
}

func TestServeInboundWebhook_missingEventTypeIsRefused(t *testing.T) {
	fc := connectortest.NewFakeControl(t, map[string]any{"vendor_secret": "s", "connection_id": "c", "enabled": true}, nil)
	t.Setenv("MCP_CONTROL_SERVER_URL", fc.Server.URL)
	t.Setenv("MCP_CONTROL_INTERNAL_TOKEN", connectortest.ConformanceInternalToken)
	spec := testSpec()
	spec.EventType = func(*http.Request, []byte) string { return "" }
	r := httptest.NewRequest(http.MethodPost, "/acme/webhook/"+connectortest.ConformanceRouteToken, strings.NewReader(`{}`))
	r.Header.Set(testSigHeader, testSign("s", []byte(`{}`)))
	rec := httptest.NewRecorder()
	testMux(spec).ServeHTTP(rec, r)
	if rec.Code != http.StatusBadRequest || len(fc.Events()) != 0 {
		t.Fatalf("status %d, events %d: an event without a type must be refused and not posted", rec.Code, len(fc.Events()))
	}
}

func TestServeInboundWebhook_routeTokenShapes(t *testing.T) {
	fc := connectortest.NewFakeControl(t, map[string]any{"vendor_secret": "s", "connection_id": "c", "enabled": true}, nil)
	t.Setenv("MCP_CONTROL_SERVER_URL", fc.Server.URL)
	t.Setenv("MCP_CONTROL_INTERNAL_TOKEN", connectortest.ConformanceInternalToken)
	h := testMux(testSpec())
	for _, path := range []string{"/acme/webhook", "/acme/webhook/", "/acme/webhook/a/b"} {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404 (no usable route token)", path, rec.Code)
		}
	}
}

func TestConstantTimeEqual(t *testing.T) {
	if !ConstantTimeEqual("abc", "abc") || ConstantTimeEqual("abc", "abd") || ConstantTimeEqual("abc", "abcd") || ConstantTimeEqual("", "x") {
		t.Fatal("ConstantTimeEqual gave a wrong answer")
	}
	if !ConstantTimeEqual("", "") {
		t.Fatal("two empty strings are equal (callers must refuse empty secrets before comparing)")
	}
}

func TestConnectorWebhookPath(t *testing.T) {
	if got := ConnectorWebhookPath(" Telegram ", "/tok/"); got != "/telegram/webhook/tok" {
		t.Fatalf("path = %q", got)
	}
	if ConnectorWebhookPath("", "tok") != "" || ConnectorWebhookPath("x", "") != "" {
		t.Fatal("empty parts must give an empty path")
	}
}

func TestServeInboundWebhook_nothingIsReadFromAnUnverifiedBody(t *testing.T) {
	// the event type and key come from the delivery; a delivery that has not been verified must not be
	// interpreted at all
	fc := connectortest.NewFakeControl(t, map[string]any{"vendor_secret": "s", "connection_id": "c", "enabled": true}, nil)
	t.Setenv("MCP_CONTROL_SERVER_URL", fc.Server.URL)
	t.Setenv("MCP_CONTROL_INTERNAL_TOKEN", connectortest.ConformanceInternalToken)
	var typeCalls, keyCalls atomic.Int32
	spec := testSpec()
	spec.EventType = func(r *http.Request, body []byte) string { typeCalls.Add(1); return "e" }
	spec.EventKey = func(r *http.Request, body []byte) string { keyCalls.Add(1); return "acme-1" }
	h := testMux(spec)
	for name, sign := range map[string]func(r *http.Request){
		"no credential":    func(r *http.Request) {},
		"wrong credential": func(r *http.Request) { r.Header.Set(testSigHeader, testSign("other", []byte(`{}`))) },
	} {
		r := httptest.NewRequest(http.MethodPost, "/acme/webhook/"+connectortest.ConformanceRouteToken, strings.NewReader(`{}`))
		sign(r)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status %d", name, rec.Code)
		}
	}
	if typeCalls.Load() != 0 || keyCalls.Load() != 0 {
		t.Fatalf("EventType called %d times and EventKey %d times for unverified deliveries", typeCalls.Load(), keyCalls.Load())
	}
}
