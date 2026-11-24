package agentruntimemcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Control's resolve-test answers 401 for a wrong internal token, 403 auth_forbidden for a service it does
// not accept, 400 for a malformed call, 404 not_found for a hidden tool or a missing server and 422 for a
// connection that holds no value for a required field. Only the last kind (and an empty config, or the
// distinct 404 connection_not_found) means the connection is gone: reading a wiring fault as "gone" would
// make a last-trigger delete drop the ingress with the vendor hooks still registered.
func TestIsConnectionUnavailable(t *testing.T) {
	serve := func(status int, body string) TriggerEnv {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		return TriggerEnv{ControlURL: srv.URL, InternalToken: "t"}
	}
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"connection_not_found", 404, `{"error":"connection_not_found","message":"connection not found"}`, true},
		{"required config field missing", 422, `{"error":"validation_error","message":"missing required config field: bot_token"}`, true},
		{"connection exists with no credentials", 200, `{"config_preview":{}}`, true},

		{"wrong internal token", 401, `{"error":"unauthorized"}`, false},
		{"service not accepted", 403, `{"error":"auth_forbidden","message":"service not allowed"}`, false},
		{"malformed call", 400, `{"error":"validation_error","message":"tenant_id is required"}`, false},
		{"tool hidden by the exposure list", 404, `{"error":"not_found","message":"tool not exposed"}`, false},
		{"server missing", 404, `{"error":"not_found","message":"mcp server not found for tenant"}`, false},
		{"other validation failure", 422, `{"error":"validation_error","message":"schema is invalid"}`, false},
		{"404 with no body (a router that lacks the route)", 404, ``, false},
		{"control failing", 500, `boom`, false},
		{"control unavailable", 503, `boom`, false},
		{"rate limited", 429, `{}`, false},
		{"timeout", 408, `{}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := serve(tc.status, tc.body)
			_, err := ResolveMCPConnectionConfig(context.Background(), env, "tenant", "", "conn", "srv", "tool")
			if err == nil {
				t.Fatal("want an error")
			}
			if got := IsConnectionUnavailable(err); got != tc.want {
				t.Fatalf("IsConnectionUnavailable = %v, want %v (err: %v)", got, tc.want, err)
			}
			_, err = ResolveMCPInstanceConfig(context.Background(), env, "tenant", "", "inst", "tool")
			if got := IsConnectionUnavailable(err); got != tc.want {
				t.Fatalf("instance: IsConnectionUnavailable = %v, want %v (err: %v)", got, tc.want, err)
			}
		})
	}
	t.Run("transport failure is not a gone connection", func(t *testing.T) {
		_, err := ResolveMCPConnectionConfig(context.Background(), TriggerEnv{ControlURL: "http://127.0.0.1:1", InternalToken: "t"}, "tenant", "", "conn", "srv", "tool")
		if err == nil || IsConnectionUnavailable(err) {
			t.Fatalf("a network failure must stay retryable: %v", err)
		}
	})
	t.Run("wrapped errors are recognised", func(t *testing.T) {
		wrapped := errors.Join(errors.New("context"), &ResolveConfigError{Status: 422, Code: "validation_error", Message: "missing required config field: api_key"})
		if !IsConnectionUnavailable(wrapped) {
			t.Fatal("errors.As must see through wrapping")
		}
		if IsConnectionUnavailable(nil) {
			t.Fatal("nil is not unavailable")
		}
	})
}
