package agentruntimemcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForwardRequestBearer_setsContext(t *testing.T) {
	var got string
	h := ForwardRequestBearer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = RequestBearerFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer pat_test_abc")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got != "pat_test_abc" {
		t.Fatalf("RequestBearerFromContext = %q, want pat_test_abc", got)
	}
}

func TestForwardRequestBearer_emptyHeader(t *testing.T) {
	var got string
	h := ForwardRequestBearer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = RequestBearerFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got != "" {
		t.Fatalf("RequestBearerFromContext = %q, want empty", got)
	}
}

func TestWithRequestBearer_emptyToken(t *testing.T) {
	ctx := WithRequestBearer(context.Background(), "  ")
	if RequestBearerFromContext(ctx) != "" {
		t.Fatal("expected empty token for whitespace-only input")
	}
}
