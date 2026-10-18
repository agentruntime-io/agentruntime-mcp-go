package agentruntimemcp

import (
	"context"
	"net/http"
	"strings"
)

type identityHeadersKey struct{}

// WithIdentityHeaders stores act-as-user headers for downstream HTTP clients during a tool call.
func WithIdentityHeaders(ctx context.Context, hdr http.Header) context.Context {
	if hdr == nil {
		return ctx
	}
	return context.WithValue(ctx, identityHeadersKey{}, hdr.Clone())
}

// IdentityHeadersFromContext returns forwarded tenant/user/project headers.
func IdentityHeadersFromContext(ctx context.Context) http.Header {
	if ctx == nil {
		return nil
	}
	v, ok := ctx.Value(identityHeadersKey{}).(http.Header)
	if !ok || v == nil {
		return nil
	}
	return v
}

// ForwardIdentityHeaders copies X-Tenant-Id / X-User-Id / X-Project-Id into request context.
func ForwardIdentityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fwd := http.Header{}
		for _, key := range []string{"X-Tenant-Id", "X-User-Id", "X-Project-Id"} {
			if v := strings.TrimSpace(r.Header.Get(key)); v != "" {
				fwd.Set(key, v)
			}
		}
		if len(fwd) > 0 {
			r = r.WithContext(WithIdentityHeaders(r.Context(), fwd))
		}
		next.ServeHTTP(w, r)
	})
}
