package relay

import (
	"os"
	"testing"
)

func TestLoadRouterEnv(t *testing.T) {
	t.Setenv("HARNESS_ORCHESTRATOR_URL", "https://api.example.com/")
	t.Setenv("MCP_CONTROL_SERVER_URL", "https://control.example.com")
	t.Setenv("MCP_CONTROL_INTERNAL_TOKEN", "secret")
	t.Setenv("RELAY_PUBLIC_BASE_URL", "https://router.example.com")
	t.Setenv("RELAY_AUTH_TOKEN", "dev-token")

	env := LoadRouterEnv()
	if env.OrchestratorURL != "https://api.example.com" {
		t.Fatalf("OrchestratorURL = %q", env.OrchestratorURL)
	}
	if env.ControlURL != "https://control.example.com" {
		t.Fatalf("ControlURL = %q", env.ControlURL)
	}
	if env.InternalToken != "secret" {
		t.Fatalf("InternalToken = %q", env.InternalToken)
	}
	if env.PublicBaseURL != "https://router.example.com" {
		t.Fatalf("PublicBaseURL = %q", env.PublicBaseURL)
	}
	if env.AuthToken != "dev-token" {
		t.Fatalf("AuthToken = %q", env.AuthToken)
	}
}

func TestServerConfigFromEnv(t *testing.T) {
	t.Setenv("HARNESS_ORCHESTRATOR_URL", "http://127.0.0.1:8001")
	t.Setenv("MCP_CONTROL_SERVER_URL", "http://127.0.0.1:8002")
	t.Setenv("MCP_CONTROL_INTERNAL_TOKEN", "tok")

	cfg := ServerConfigFromEnv()
	if cfg.OrchestratorCallback == nil {
		t.Fatal("expected OrchestratorCallback")
	}
	if cfg.ControlCallback == nil {
		t.Fatal("expected ControlCallback")
	}
}

func TestPublicBaseURLFromEnvNoFallback(t *testing.T) {
	t.Setenv("RELAY_PUBLIC_BASE_URL", "")
	t.Setenv("HARNESS_RELAY_URL", "http://127.0.0.1:8340")
	if got := PublicBaseURLFromEnv(nil); got != "" {
		t.Fatalf("PublicBaseURLFromEnv should not fall back to HARNESS_RELAY_URL, got %q", got)
	}
	t.Setenv("RELAY_PUBLIC_BASE_URL", "https://router.example.com")
	if got := PublicBaseURLFromEnv(os.Getenv); got != "https://router.example.com" {
		t.Fatalf("got %q", got)
	}
}
