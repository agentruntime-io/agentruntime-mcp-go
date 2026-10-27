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

func TestLoadRouterEnv_authSettings(t *testing.T) {
	t.Setenv("RELAY_AUTH_BFF_URL", "https://api.example.com/")
	t.Setenv("RELAY_AUTH_WHEELHOUSE_URL", "https://wheelhouse.example.com")
	t.Setenv("RELAY_AUTH_WHEELHOUSE_INTERNAL_TOKEN", "wh-secret")
	t.Setenv("RELAY_ALLOW_INSECURE", "true")
	t.Setenv("RELAY_ALLOW_LEGACY_DISPATCH", "1")
	t.Setenv("MCP_CONTROL_INTERNAL_TOKEN", "internal")

	env := LoadRouterEnv()
	if env.AuthBFFURL != "https://api.example.com" || env.AuthWheelhouseURL != "https://wheelhouse.example.com" || env.AuthWheelhouseToken != "wh-secret" {
		t.Fatalf("auth backends = %+v", env)
	}
	if !env.AllowInsecure || !env.AllowLegacyDispatch {
		t.Fatalf("flags = insecure %v legacy %v", env.AllowInsecure, env.AllowLegacyDispatch)
	}
	cfg := ServerConfigFromEnv()
	if cfg.Verifier == nil || cfg.InternalToken != "internal" || !cfg.AllowInsecure || !cfg.AllowLegacyDispatch {
		t.Fatalf("ServerConfig = %+v", cfg)
	}
}

func TestServerConfigFromEnv_secureByDefault(t *testing.T) {
	for _, k := range []string{"RELAY_AUTH_BFF_URL", "RELAY_AUTH_WHEELHOUSE_URL", "RELAY_AUTH_WHEELHOUSE_INTERNAL_TOKEN", "RELAY_ALLOW_INSECURE", "RELAY_ALLOW_LEGACY_DISPATCH", "RELAY_AUTH_TOKEN", "MCP_CONTROL_INTERNAL_TOKEN"} {
		t.Setenv(k, "")
	}
	cfg := ServerConfigFromEnv()
	if cfg.Verifier != nil || cfg.AllowInsecure || cfg.AllowLegacyDispatch {
		t.Fatalf("unset env must leave the relay locked down, got %+v", cfg)
	}
}

func TestNewVerifier_wheelhouseNeedsItsToken(t *testing.T) {
	if v := NewVerifier(VerifierConfig{WheelhouseURL: "https://wheelhouse.example.com"}); v != nil {
		t.Fatal("PAT validation without the Wheelhouse token must not be enabled")
	}
	if v := NewVerifier(VerifierConfig{}); v != nil {
		t.Fatal("no backends means no verifier")
	}
}
