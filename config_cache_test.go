package agentruntimemcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfigCacheKey_Stable(t *testing.T) {
	schema := map[string]any{"api_key": map[string]any{"type": "string"}}
	ctx := map[string]any{"instance_id": "inst-1"}
	k1 := configCacheKey("token-abc", schema, ctx)
	k2 := configCacheKey("token-abc", schema, ctx)
	if k1 != k2 {
		t.Fatalf("keys differ: %q vs %q", k1, k2)
	}
	if k1 == "" {
		t.Fatal("expected non-empty key")
	}
}

func TestRetryAfterFromControlBody(t *testing.T) {
	body := `{"error":"rate_limited","details":{"retry_after":12}}`
	if got := retryAfterFromControlBody(body); got != 12 {
		t.Fatalf("got %d want 12", got)
	}
}

func TestFetchControlConfigCached_SingleflightAndCache(t *testing.T) {
	resetConfigCacheForTest()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"config": map[string]any{"api_key": "secret"},
		})
	}))
	defer srv.Close()

	t.Setenv("MCP_CONTROL_SERVER_URL", srv.URL)
	t.Setenv("MCP_CONFIG_CACHE_TTL_SEC", "60")
	t.Setenv("MCP_CONFIG_RETRY_BUDGET_SEC", "5")

	schema := map[string]any{"api_key": map[string]any{"type": "string"}}
	ctx := map[string]any{"instance_id": "inst-1", "tool_name": "send"}

	done := make(chan ConfigView, 10)
	for i := 0; i < 10; i++ {
		go func() {
			cfg, err := fetchControlConfigCached("run-token", schema, ctx)
			if err != nil {
				t.Error(err)
				done <- nil
				return
			}
			done <- cfg
		}()
	}
	for i := 0; i < 10; i++ {
		cfg := <-done
		if cfg == nil {
			t.Fatal("nil config")
		}
		if cfg["api_key"] != "secret" {
			t.Fatalf("api_key = %v", cfg["api_key"])
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 control call, got %d", got)
	}

	cfg2, err := fetchControlConfigCached("run-token", schema, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2["api_key"] != "secret" {
		t.Fatalf("cached api_key = %v", cfg2["api_key"])
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected cache hit without extra call, got %d calls", got)
	}
}

func TestFetchControlConfigWithRetry_429ThenSuccess(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate_limited","details":{"retry_after":1}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"config": map[string]any{"api_key": "ok"},
		})
	}))
	defer srv.Close()

	t.Setenv("MCP_CONTROL_SERVER_URL", srv.URL)
	t.Setenv("MCP_CONFIG_RETRY_BUDGET_SEC", "10")

	start := time.Now()
	cfg, err := fetchControlConfigWithRetry("tok", map[string]any{"api_key": map[string]any{"type": "string"}}, map[string]any{"instance_id": "i1"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg["api_key"] != "ok" {
		t.Fatalf("cfg = %v", cfg)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d want 2", calls.Load())
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("expected wait >= 1s, got %v", elapsed)
	}
}

func TestRateLimitWaitDuration_UsesRetryAfter(t *testing.T) {
	wait := rateLimitWaitDuration(&ControlError{RetryAfterSec: 15}, 0)
	if wait != 15*time.Second {
		t.Fatalf("wait = %v", wait)
	}
}
