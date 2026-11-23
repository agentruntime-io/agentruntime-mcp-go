package agentruntimemcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
		{"connection not found", 404, `{"error":"not_found"}`, true},
		{"connection forbidden", 403, `{}`, true},
		{"credentials gone", 401, `{}`, true},
		{"invalid connection", 422, `{}`, true},
		{"connection exists with no credentials", 200, `{"config_preview":{}}`, true},
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
		wrapped := errors.Join(errors.New("context"), &ResolveConfigError{Status: 404})
		if !IsConnectionUnavailable(wrapped) {
			t.Fatal("errors.As must see through wrapping")
		}
		if IsConnectionUnavailable(nil) {
			t.Fatal("nil is not unavailable")
		}
	})
}
