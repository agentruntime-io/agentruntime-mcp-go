package agentruntimemcp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOAuthMiddleware_toolsListWithoutAuth(t *testing.T) {
	cfg := OAuthConfig{
		ResourceURL:          "https://mcp.example.com",
		Issuer:               "https://auth.example.com/realms/test",
		JWKSURL:              "https://auth.example.com/jwks",
		Audience:             "https://mcp.example.com",
		RequiredScopes:       []string{"mcp:execute"},
		AuthorizationServers: []string{"https://auth.example.com/realms/test"},
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
		ResourceURL:          "https://mcp.example.com",
		Issuer:               "https://auth.example.com/realms/test",
		JWKSURL:              "https://auth.example.com/jwks",
		Audience:             "https://mcp.example.com",
		RequiredScopes:       []string{"mcp:execute"},
		AuthorizationServers: []string{"https://auth.example.com/realms/test"},
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

func TestOAuthProtectedResourceMetadataURL(t *testing.T) {
	cases := map[string]string{
		"https://mcp.example.com":      "https://mcp.example.com/.well-known/oauth-protected-resource",
		"https://mcp.example.com/":     "https://mcp.example.com/.well-known/oauth-protected-resource",
		"https://mcp.example.com/mcp":  "https://mcp.example.com/.well-known/oauth-protected-resource/mcp",
		"https://mcp.example.com/a/b/": "https://mcp.example.com/.well-known/oauth-protected-resource/a/b",
	}
	for resource, want := range cases {
		if got := OAuthProtectedResourceMetadataURL(resource); got != want {
			t.Errorf("OAuthProtectedResourceMetadataURL(%q)=%q want %q", resource, got, want)
		}
	}
}

func TestOAuthProtectedResourcePaths(t *testing.T) {
	root := OAuthProtectedResourcePaths(OAuthConfig{ResourceURL: "https://mcp.example.com"})
	if len(root) != 1 || root[0] != OAuthProtectedResourcePath {
		t.Fatalf("root resource paths=%v", root)
	}
	withPath := OAuthProtectedResourcePaths(OAuthConfig{ResourceURL: "https://mcp.example.com/mcp"})
	if len(withPath) != 2 || withPath[1] != OAuthProtectedResourcePath+"/mcp" {
		t.Fatalf("path resource paths=%v", withPath)
	}
}

func pathResourceConfig() OAuthConfig {
	return OAuthConfig{
		ResourceURL:          "https://mcp.example.com/mcp",
		Issuer:               "https://auth.example.com/realms/test",
		JWKSURL:              "https://auth.example.com/jwks",
		Audience:             "https://mcp.example.com",
		RequiredScopes:       []string{"mcp:execute"},
		AuthorizationServers: []string{"https://auth.example.com/realms/test"},
	}
}

func TestOAuthMiddleware_challengeUsesPathInsertedMetadataURL(t *testing.T) {
	cfg := pathResourceConfig()
	handler := OAuthMiddleware(cfg, "/mcp", newJWTVerifier(cfg.Issuer, cfg.Audience, cfg.JWKSURL), http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x"}}`)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	want := `Bearer resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource/mcp", scope="mcp:execute"`
	if got := rec.Header().Get("WWW-Authenticate"); got != want {
		t.Fatalf("WWW-Authenticate=%q want %q", got, want)
	}
}

func TestOAuthMiddleware_requireAuthOnConnect(t *testing.T) {
	cfg := pathResourceConfig()
	cfg.RequireAuthOnConnect = true
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	handler := OAuthMiddleware(cfg, "/mcp", newJWTVerifier(cfg.Issuer, cfg.Audience, cfg.JWKSURL), inner)

	for _, method := range []string{"initialize", "tools/list", "ping"} {
		body := []byte(`{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":{}}`)
		req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") == "" {
			t.Fatalf("%s: status=%d challenge=%q, want 401 with challenge", method, rec.Code, rec.Header().Get("WWW-Authenticate"))
		}
	}
	if called {
		t.Fatal("handshake reached the MCP handler without a token")
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)))
	req.Header.Set("Authorization", "Bearer pat_example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if !called {
		t.Fatal("PAT bearer should pass in strict mode")
	}
}

func TestOAuthMiddleware_servesPathInsertedMetadata(t *testing.T) {
	cfg := pathResourceConfig()
	handler := OAuthMiddleware(cfg, "/mcp", newJWTVerifier(cfg.Issuer, cfg.Audience, cfg.JWKSURL), http.NotFoundHandler())
	for _, path := range OAuthProtectedResourcePaths(cfg) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"resource":"https://mcp.example.com/mcp"`)) {
			t.Fatalf("%s: status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestOAuthConfigFromEnv_requireAuthOnConnect(t *testing.T) {
	t.Setenv(envOAuthRequireAuthOnConnect, "true")
	if !OAuthConfigFromEnv().RequireAuthOnConnect {
		t.Fatal("expected strict mode from env")
	}
	t.Setenv(envOAuthRequireAuthOnConnect, "")
	if OAuthConfigFromEnv().RequireAuthOnConnect {
		t.Fatal("strict mode must default off")
	}
}

func TestOAuthMiddleware_requireAuthOnConnectProbes(t *testing.T) {
	cfg := pathResourceConfig()
	cfg.RequireAuthOnConnect = true
	handler := OAuthMiddleware(cfg, "/mcp", newJWTVerifier(cfg.Issuer, cfg.Audience, cfg.JWKSURL), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	// Auggie and Codex discover with HEAD/GET, Gemini CLI with HEAD.
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(method, "/mcp", nil))
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") == "" {
			t.Fatalf("%s: status=%d, want 401 with challenge", method, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodOptions, "/mcp", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS: status=%d, preflight must pass through", rec.Code)
	}
}
