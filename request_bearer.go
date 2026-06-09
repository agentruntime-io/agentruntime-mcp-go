package agentruntimemcp

import (
	"context"
	"net/http"
	"strings"
)

type requestBearerKey struct{}

// WithRequestBearer stores the incoming MCP caller bearer token for downstream HTTP clients.
func WithRequestBearer(ctx context.Context, token string) context.Context {
	token = strings.TrimSpace(token)
	if token == "" {
		return ctx
	}
	return context.WithValue(ctx, requestBearerKey{}, token)
}

// RequestBearerFromContext returns the bearer token from the incoming MCP request, if set.
func RequestBearerFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, ok := ctx.Value(requestBearerKey{}).(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(v)
}

// ForwardRequestBearer copies Authorization: Bearer (or X-MCP-Token) into request context.
func ForwardRequestBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token := extractToken(r); token != "" {
			r = r.WithContext(WithRequestBearer(r.Context(), token))
		}
		next.ServeHTTP(w, r)
	})
}
