package agentruntimemcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddleware_EnvOnlyNoControlURL_ConfigRequired(t *testing.T) {
	t.Setenv("MCP_CONFIG_FETCH_REQUIRED", "true")
	t.Setenv("MCP_CONTROL_SERVER_URL", "")
	t.Setenv("API_KEY", "secret")
	t.Setenv("FROM_EMAIL", "from@test.com")

	schema := map[string]any{
		"api_key":    map[string]any{"type": "string", "required": true},
		"from_email": map[string]any{"type": "string", "required": true},
	}

	var got ConfigView
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = ConfigFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	h := Middleware(schema, next, "/mcp")
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x","arguments":{}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if got["api_key"] != "secret" {
		t.Fatalf("api_key: %v", got["api_key"])
	}
	if got["from_email"] != "from@test.com" {
		t.Fatalf("from_email: %v", got["from_email"])
	}
}

func TestMiddleware_EnvOnlyMissingRequired_ConfigRequired(t *testing.T) {
	t.Setenv("MCP_CONFIG_FETCH_REQUIRED", "true")
	t.Setenv("MCP_CONTROL_SERVER_URL", "")
	t.Setenv("API_KEY", "secret")
	t.Setenv("FROM_EMAIL", "") // unset

	schema := map[string]any{
		"api_key":    map[string]any{"type": "string", "required": true},
		"from_email": map[string]any{"type": "string", "required": true},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next should not run")
	})

	h := Middleware(schema, next, "/mcp")
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestMiddleware_InitializeSkipsConfigMergePath(t *testing.T) {
	t.Setenv("MCP_CONFIG_FETCH_REQUIRED", "true")
	t.Setenv("MCP_CONTROL_SERVER_URL", "")

	schema := map[string]any{
		"api_key": map[string]any{"type": "string", "required": true},
	}

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	h := Middleware(schema, next, "/mcp")
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("next not called")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
}
