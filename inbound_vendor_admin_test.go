package agentruntimemcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const adminToken = "router-internal-token"

type adminCalls struct {
	register, status, unregister []InboundVendorRequest
}

func newAdminMux(t *testing.T, calls *adminCalls) *http.ServeMux {
	t.Helper()
	t.Setenv("MCP_CONTROL_SERVER_URL", "http://control.invalid")
	t.Setenv("MCP_CONTROL_INTERNAL_TOKEN", adminToken)
	mux := http.NewServeMux()
	MountInboundVendorRoutes(mux, InboundVendorSpec{
		Adapter: "acme",
		Register: func(ctx context.Context, env TriggerEnv, req InboundVendorRequest) (InboundRegisterResult, error) {
			calls.register = append(calls.register, req)
			if req.Scope["bad"] == true {
				return InboundRegisterResult{}, InboundBadRequest("scope.bad is not allowed")
			}
			if req.Scope["vendor_down"] == true {
				return InboundRegisterResult{}, errors.New("vendor said 500")
			}
			if req.Scope["echo_secret"] == true {
				return InboundRegisterResult{}, errors.New("vendor rejected secret " + req.VendorSecret + " for the hook")
			}
			res := InboundRegisterResult{
				WebhookURL:      req.WebhookURL,
				RegisteredScope: req.Scope,
				VendorIDs:       map[string]any{"hook_id": float64(42)},
			}
			if req.Scope["generated_secret"] == true {
				res.VendorSecret = "vendor-made-secret"
			}
			return res, nil
		},
		Status: func(ctx context.Context, env TriggerEnv, req InboundVendorRequest) (InboundStatusResult, error) {
			calls.status = append(calls.status, req)
			if req.Scope["unknown"] == true {
				return InboundStatusResult{Note: "no list call"}, nil
			}
			return InboundStatusResult{Registered: BoolPtr(true), WebhookURL: "https://router.example.com/acme/webhook/" + req.RouteToken, RegisteredScope: req.Scope, VendorIDs: req.VendorIDs, Note: "ok"}, nil
		},
		Unregister: func(ctx context.Context, env TriggerEnv, req InboundVendorRequest) (InboundUnregisterResult, error) {
			calls.unregister = append(calls.unregister, req)
			return InboundUnregisterResult{Removed: true}, nil
		},
	})
	return mux
}

func adminReq(method, path string, body any, token string, headers map[string]string) *http.Request {
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	r := httptest.NewRequest(method, path, rdr)
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("X-Internal-Token", token)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

var tenantHeaders = map[string]string{"X-Tenant-Id": "tenant-9", "X-Project-Id": "project-3"}

func serve(mux http.Handler, r *http.Request) (*httptest.ResponseRecorder, map[string]any) {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func validRegisterBody() map[string]any {
	return map[string]any{
		"connection_id": "conn-1",
		"server_id":     "srv-1",
		"scope":         map[string]any{"events": []string{"push"}},
		"webhook_url":   "https://router.example.com/acme/webhook/tok-1",
		"vendor_secret": "s3cret",
	}
}

func TestInboundVendorRegister_passesContractRequestAndReturnsContractResponse(t *testing.T) {
	var calls adminCalls
	mux := newAdminMux(t, &calls)
	rec, out := serve(mux, adminReq("POST", "/acme/inbound/register-webhook/tok-1", validRegisterBody(), adminToken, tenantHeaders))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if len(calls.register) != 1 {
		t.Fatalf("register called %d times", len(calls.register))
	}
	got := calls.register[0]
	if got.RouteToken != "tok-1" || got.ConnectionID != "conn-1" || got.ServerID != "srv-1" ||
		got.WebhookURL != "https://router.example.com/acme/webhook/tok-1" || got.VendorSecret != "s3cret" ||
		got.Tenant.TenantID != "tenant-9" || got.Tenant.ProjectID != "project-3" {
		t.Fatalf("request = %+v", got)
	}
	if out["ok"] != true || out["webhook_url"] != "https://router.example.com/acme/webhook/tok-1" {
		t.Fatalf("response = %v", out)
	}
	if ids, _ := out["vendor_ids"].(map[string]any); ids["hook_id"] != float64(42) {
		t.Fatalf("vendor_ids = %v", out["vendor_ids"])
	}
	if _, ok := out["registered_scope"].(map[string]any); !ok {
		t.Fatalf("registered_scope missing: %v", out)
	}
	if _, present := out["vendor_secret"]; present {
		t.Fatalf("vendor_secret must only be returned when the vendor generated it: %v", out)
	}
}

func TestInboundVendorRegister_returnsVendorGeneratedSecret(t *testing.T) {
	var calls adminCalls
	mux := newAdminMux(t, &calls)
	body := validRegisterBody()
	body["scope"] = map[string]any{"generated_secret": true}
	_, out := serve(mux, adminReq("POST", "/acme/inbound/register-webhook/tok-1", body, adminToken, tenantHeaders))
	if out["vendor_secret"] != "vendor-made-secret" {
		t.Fatalf("response = %v", out)
	}
}

func TestInboundVendorRoutes_authentication(t *testing.T) {
	var calls adminCalls
	mux := newAdminMux(t, &calls)
	paths := map[string]string{
		"POST": "/acme/inbound/register-webhook/tok-1",
	}
	for _, token := range []string{"", "wrong", adminToken + "x"} {
		rec, _ := serve(mux, adminReq("POST", paths["POST"], validRegisterBody(), token, tenantHeaders))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("token %q: status %d, want 401", token, rec.Code)
		}
	}
	for _, p := range []string{"/acme/inbound/webhook-status/tok-1?connection_id=c&server_id=s&webhook_url=" + url.QueryEscape("https://x/acme/webhook/tok-1")} {
		rec, _ := serve(mux, adminReq("GET", p, nil, "wrong", tenantHeaders))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status route accepted a wrong token: %d", rec.Code)
		}
	}
	rec, _ := serve(mux, adminReq("POST", "/acme/inbound/unregister-webhook/tok-1", validRegisterBody(), "", tenantHeaders))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unregister accepted no token: %d", rec.Code)
	}
	if len(calls.register)+len(calls.status)+len(calls.unregister) != 0 {
		t.Fatalf("an unauthenticated call reached the connector: %+v", calls)
	}
}

func TestInboundVendorRoutes_tokenNotConfiguredFailsClosed(t *testing.T) {
	var calls adminCalls
	mux := newAdminMux(t, &calls)
	t.Setenv("MCP_CONTROL_INTERNAL_TOKEN", "")
	rec, _ := serve(mux, adminReq("POST", "/acme/inbound/register-webhook/tok-1", validRegisterBody(), "", tenantHeaders))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no configured token and no header must be refused, got %d", rec.Code)
	}
}

func TestInboundVendorRegister_validation(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		mutate func(b map[string]any)
		hdr    map[string]string
	}{
		{"no connection", "/acme/inbound/register-webhook/tok-1", func(b map[string]any) { delete(b, "connection_id") }, tenantHeaders},
		{"no server", "/acme/inbound/register-webhook/tok-1", func(b map[string]any) { delete(b, "server_id") }, tenantHeaders},
		{"no tenant header", "/acme/inbound/register-webhook/tok-1", func(b map[string]any) {}, map[string]string{}},
		{"no webhook url", "/acme/inbound/register-webhook/tok-1", func(b map[string]any) { delete(b, "webhook_url") }, tenantHeaders},
		{"no vendor secret", "/acme/inbound/register-webhook/tok-1", func(b map[string]any) { delete(b, "vendor_secret") }, tenantHeaders},
		{"url for another token", "/acme/inbound/register-webhook/tok-2", func(b map[string]any) {}, tenantHeaders},
		{"url for another adapter", "/acme/inbound/register-webhook/tok-1", func(b map[string]any) {
			b["webhook_url"] = "https://router.example.com/github/webhook/tok-1"
		}, tenantHeaders},
		{"url with a query", "/acme/inbound/register-webhook/tok-1", func(b map[string]any) {
			b["webhook_url"] = "https://router.example.com/acme/webhook/tok-1?x=1"
		}, tenantHeaders},
		{"url not http", "/acme/inbound/register-webhook/tok-1", func(b map[string]any) {
			b["webhook_url"] = "ftp://router.example.com/acme/webhook/tok-1"
		}, tenantHeaders},
		{"no route token", "/acme/inbound/register-webhook/", func(b map[string]any) {}, tenantHeaders},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls adminCalls
			mux := newAdminMux(t, &calls)
			body := validRegisterBody()
			tc.mutate(body)
			rec, out := serve(mux, adminReq("POST", tc.path, body, adminToken, tc.hdr))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d (%v), want 400", rec.Code, out)
			}
			if len(calls.register) != 0 {
				t.Fatalf("an invalid request reached the connector")
			}
		})
	}
}

func TestInboundVendorRegister_errorMapping(t *testing.T) {
	var calls adminCalls
	mux := newAdminMux(t, &calls)
	body := validRegisterBody()
	body["scope"] = map[string]any{"bad": true}
	rec, out := serve(mux, adminReq("POST", "/acme/inbound/register-webhook/tok-1", body, adminToken, tenantHeaders))
	if rec.Code != http.StatusBadRequest || !strings.Contains(out["error"].(string), "scope.bad") {
		t.Fatalf("a bad scope is a permanent 400: %d %v", rec.Code, out)
	}
	body["scope"] = map[string]any{"vendor_down": true}
	rec, out = serve(mux, adminReq("POST", "/acme/inbound/register-webhook/tok-1", body, adminToken, tenantHeaders))
	if rec.Code != http.StatusBadGateway || !strings.Contains(out["error"].(string), "vendor said 500") {
		t.Fatalf("a vendor failure is a retryable 502: %d %v", rec.Code, out)
	}
}

func TestInboundVendorRegister_methodNotAllowed(t *testing.T) {
	var calls adminCalls
	mux := newAdminMux(t, &calls)
	rec, _ := serve(mux, adminReq("GET", "/acme/inbound/register-webhook/tok-1", nil, adminToken, tenantHeaders))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestInboundVendorUnregister(t *testing.T) {
	var calls adminCalls
	mux := newAdminMux(t, &calls)
	body := validRegisterBody()
	delete(body, "vendor_secret") // unregister does not need it
	body["vendor_ids"] = map[string]any{"webhook_id": "w1"}
	rec, out := serve(mux, adminReq("POST", "/acme/inbound/unregister-webhook/tok-1", body, adminToken, tenantHeaders))
	if rec.Code != 200 || out["ok"] != true || out["removed"] != true {
		t.Fatalf("%d %v", rec.Code, out)
	}
	if len(calls.unregister) != 1 || calls.unregister[0].VendorIDs["webhook_id"] != "w1" {
		t.Fatalf("vendor_ids must reach the connector: %+v", calls.unregister)
	}
}

func TestInboundVendorStatus(t *testing.T) {
	var calls adminCalls
	mux := newAdminMux(t, &calls)
	scope, _ := json.Marshal(map[string]any{"repo": "o/r"})
	ids, _ := json.Marshal(map[string]any{"hook_id": 5})
	q := url.Values{
		"connection_id": {"conn-1"}, "server_id": {"srv-1"},
		"webhook_url": {"https://router.example.com/acme/webhook/tok-1"},
		"scope":       {string(scope)}, "vendor_ids": {string(ids)},
	}
	rec, out := serve(mux, adminReq("GET", "/acme/inbound/webhook-status/tok-1?"+q.Encode(), nil, adminToken, tenantHeaders))
	if rec.Code != 200 {
		t.Fatalf("%d %v", rec.Code, out)
	}
	if out["registered"] != true || out["webhook_url"] != "https://router.example.com/acme/webhook/tok-1" || out["ok"] != true {
		t.Fatalf("response = %v", out)
	}
	got := calls.status[0]
	if got.Scope["repo"] != "o/r" || got.VendorIDs["hook_id"] != float64(5) || got.Tenant.TenantID != "tenant-9" {
		t.Fatalf("request = %+v", got)
	}
	q.Set("scope", "{not json")
	rec, _ = serve(mux, adminReq("GET", "/acme/inbound/webhook-status/tok-1?"+q.Encode(), nil, adminToken, tenantHeaders))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed scope JSON: status %d, want 400", rec.Code)
	}
}

func TestInboundVendorPaths(t *testing.T) {
	if InboundRegisterWebhookPath("Acme", "t") != "/acme/inbound/register-webhook/t" ||
		InboundWebhookStatusPath("acme", "t") != "/acme/inbound/webhook-status/t" ||
		InboundUnregisterWebhookPath("acme", "t") != "/acme/inbound/unregister-webhook/t" {
		t.Fatal("path builders must match the manifest path templates")
	}
}

func TestInboundVendorStatus_minimalQueryAndUnknownState(t *testing.T) {
	var calls adminCalls
	mux := newAdminMux(t, &calls)
	// control's status call carries only connection_id and server_id.
	rec, out := serve(mux, adminReq("GET", "/acme/inbound/webhook-status/tok-1?connection_id=c&server_id=s", nil, adminToken, tenantHeaders))
	if rec.Code != 200 || out["registered"] != true {
		t.Fatalf("%d %v", rec.Code, out)
	}
	scope, _ := json.Marshal(map[string]any{"unknown": true})
	rec, out = serve(mux, adminReq("GET", "/acme/inbound/webhook-status/tok-1?connection_id=c&server_id=s&scope="+url.QueryEscape(string(scope)), nil, adminToken, tenantHeaders))
	if rec.Code != 200 {
		t.Fatalf("%d %v", rec.Code, out)
	}
	if _, present := out["registered"]; present {
		t.Fatalf("a connector that cannot tell must omit registered (absent means unknown, not stale): %v", out)
	}
	if _, present := out["webhook_url"]; present {
		t.Fatalf("webhook_url must be absent when unknown: %v", out)
	}
}

func TestInboundVendorUnregister_urlOptionalButCheckedWhenGiven(t *testing.T) {
	var calls adminCalls
	mux := newAdminMux(t, &calls)
	body := map[string]any{"connection_id": "c", "server_id": "s", "scope": map[string]any{}}
	rec, out := serve(mux, adminReq("POST", "/acme/inbound/unregister-webhook/tok-1", body, adminToken, tenantHeaders))
	if rec.Code != 200 || out["ok"] != true {
		t.Fatalf("unregister without a url: %d %v", rec.Code, out)
	}
	body["webhook_url"] = "https://router.example.com/github/webhook/tok-1"
	rec, _ = serve(mux, adminReq("POST", "/acme/inbound/unregister-webhook/tok-1", body, adminToken, tenantHeaders))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a url for another adapter must be refused: %d", rec.Code)
	}
}

func TestInboundVendorTenantFromBodyWhenNoHeader(t *testing.T) {
	var calls adminCalls
	mux := newAdminMux(t, &calls)
	body := validRegisterBody()
	body["tenant_id"] = "tenant-body"
	rec, _ := serve(mux, adminReq("POST", "/acme/inbound/register-webhook/tok-1", body, adminToken, nil))
	if rec.Code != 200 || calls.register[0].Tenant.TenantID != "tenant-body" {
		t.Fatalf("%d %+v", rec.Code, calls.register)
	}
}

func TestInboundVendorRegister_errorsNeverEchoTheVendorSecret(t *testing.T) {
	var calls adminCalls
	mux := newAdminMux(t, &calls)
	body := validRegisterBody()
	body["scope"] = map[string]any{"echo_secret": true}
	body["vendor_secret"] = "super-secret-value"
	rec, out := serve(mux, adminReq("POST", "/acme/inbound/register-webhook/tok-1", body, adminToken, tenantHeaders))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("%d %v", rec.Code, out)
	}
	if strings.Contains(rec.Body.String(), "super-secret-value") {
		t.Fatalf("the secret control sent came back in the error: %s", rec.Body.String())
	}
}

func TestInboundVendorRegister_webhookURLMustBeHTTPSOutsideLoopback(t *testing.T) {
	mk := func(url string) int {
		var calls adminCalls
		mux := newAdminMux(t, &calls)
		body := validRegisterBody()
		body["webhook_url"] = url
		rec, _ := serve(mux, adminReq("POST", "/acme/inbound/register-webhook/tok-1", body, adminToken, tenantHeaders))
		return rec.Code
	}
	for _, u := range []string{"http://router.example.com/acme/webhook/tok-1", "http://10.0.0.5/acme/webhook/tok-1"} {
		if code := mk(u); code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400 (a vendor webhook URL must be https)", u, code)
		}
	}
	for _, u := range []string{
		"https://router.example.com/acme/webhook/tok-1",
		"http://127.0.0.1:8021/acme/webhook/tok-1",
		"http://localhost:8021/acme/webhook/tok-1",
		"http://[::1]:8021/acme/webhook/tok-1",
	} {
		if code := mk(u); code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", u, code)
		}
	}
}
