package agentruntimemcp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOAuthMiddleware_toolsListWithoutAuth(t *testing.T) {
	cfg := OAuthConfig{
		ResourceURL:           "https://mcp.example.com",
		Issuer:                "https://auth.example.com/realms/test",
		JWKSURL:               "https://auth.example.com/jwks",
		Audience:              "https://mcp.example.com",
		RequiredScopes:        []string{"mcp:execute"},
		AuthorizationServers:  []string{"https://auth.example.com/realms/test"},
	}
	verifier := newJWTVerifier(cfg.Issuer, cfg.Audience, cfg.JWKSURL)

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := OAuthMiddleware(cfg, "/mcp", verifier, inner)

	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected tools/list to pass without bearer token")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", rec.Code)
	}
}

func TestOAuthMiddleware_toolsCallRequiresAuth(t *testing.T) {
	cfg := OAuthConfig{
		ResourceURL:           "https://mcp.example.com",
		Issuer:                "https://auth.example.com/realms/test",
		JWKSURL:               "https://auth.example.com/jwks",
		Audience:              "https://mcp.example.com",
		RequiredScopes:        []string{"mcp:execute"},
		AuthorizationServers:  []string{"https://auth.example.com/realms/test"},
	}
	verifier := newJWTVerifier(cfg.Issuer, cfg.Audience, cfg.JWKSURL)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := OAuthMiddleware(cfg, "/mcp", verifier, inner)

	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x"}}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got == "" {
		t.Fatal("expected WWW-Authenticate challenge")
	}
}

func TestOAuthProtectedResourceHandler(t *testing.T) {
	cfg := OAuthConfig{
		ResourceURL:          "https://mcp.example.com",
		AuthorizationServers: []string{"https://auth.example.com/realms/test"},
		Issuer:               "https://auth.example.com/realms/test",
		JWKSURL:              "https://auth.example.com/jwks",
		Audience:             "https://mcp.example.com",
	}
	req := httptest.NewRequest(http.MethodGet, OAuthProtectedResourcePath, nil)
	rec := httptest.NewRecorder()
	OAuthProtectedResourceHandler(cfg).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type=%q", ct)
	}
}

func TestOAuthMiddleware_disabledIsNoOp(t *testing.T) {
	cfg := OAuthConfig{}
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := OAuthMiddleware(cfg, "/mcp", nil, inner)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(`{"method":"tools/call"}`)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected pass-through when oauth disabled")
	}
}
