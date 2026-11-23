package agentruntimemcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type triggerTestAdapter struct {
	manifest TriggerManifest
	webhook  bool
	vendor   bool
}

func (triggerTestAdapter) Register(*mcp.Server, SchemaWriter) {}
func (a triggerTestAdapter) TriggerManifest() TriggerManifest { return a.manifest }
func (a triggerTestAdapter) RegisterWebhook(mux *http.ServeMux) {
	MountInboundWebhook(mux, InboundWebhookSpec{Adapter: "telegram"})
}
func (a triggerTestAdapter) RegisterInboundVendor(mux *http.ServeMux) {
	MountInboundVendorRoutes(mux, InboundVendorSpec{Adapter: "telegram"})
}

func TestMountAdapterExtras_servesManifestBehindInternalToken(t *testing.T) {
	t.Setenv("MCP_CONTROL_INTERNAL_TOKEN", "router-token")
	mux := http.NewServeMux()
	if err := mountAdapterExtras(mux, "telegram", triggerTestAdapter{manifest: validTestManifest()}); err != nil {
		t.Fatal(err)
	}

	get := func(token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/telegram/triggers", nil)
		if token != "" {
			r.Header.Set("X-Internal-Token", token)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		return rec
	}
	if rec := get(""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rec.Code)
	}
	if rec := get("wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", rec.Code)
	}
	rec := get("router-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content type %q", ct)
	}
	var m TriggerManifest
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.Label != "Telegram" || len(m.Events) != 1 || m.Events[0].ID != "message" {
		t.Fatalf("manifest = %+v", m)
	}
	if strings.Contains(rec.Body.String(), `"fields":null`) {
		t.Fatalf("served manifest must normalise null lists: %s", rec.Body.String())
	}

	post := httptest.NewRequest(http.MethodPost, "/telegram/triggers", nil)
	post.Header.Set("X-Internal-Token", "router-token")
	prec := httptest.NewRecorder()
	mux.ServeHTTP(prec, post)
	if prec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", prec.Code)
	}
}

func TestMountAdapterExtras_tokenNotConfiguredFailsClosed(t *testing.T) {
	t.Setenv("MCP_CONTROL_INTERNAL_TOKEN", "")
	mux := http.NewServeMux()
	if err := mountAdapterExtras(mux, "telegram", triggerTestAdapter{manifest: validTestManifest()}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/telegram/triggers", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("an unconfigured router must not serve the manifest: %d", rec.Code)
	}
}

func TestMountAdapterExtras_invalidManifestFailsStartup(t *testing.T) {
	bad := validTestManifest()
	bad.Delivery.Path = "/somewhere/else"
	err := mountAdapterExtras(http.NewServeMux(), "telegram", triggerTestAdapter{manifest: bad})
	if err == nil || !strings.Contains(err.Error(), "delivery.path") {
		t.Fatalf("a manifest control would refuse must fail the router at startup, got %v", err)
	}
}

func TestMountAdapterExtras_mountsWebhookAndVendorRoutes(t *testing.T) {
	mux := http.NewServeMux()
	if err := mountAdapterExtras(mux, "telegram", triggerTestAdapter{manifest: validTestManifest()}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/telegram/webhook/tok", "/telegram/inbound/register-webhook/tok", "/telegram/inbound/unregister-webhook/tok"} {
		_, pattern := mux.Handler(httptest.NewRequest(http.MethodPost, path, nil))
		if pattern == "" {
			t.Errorf("%s is not routed", path)
		}
	}
}
