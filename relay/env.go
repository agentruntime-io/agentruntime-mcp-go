package relay

import (
	"log"
	"os"
	"strings"
)

// RouterEnv is relay configuration for the go-connectors router process.
// One canonical variable per setting — no fallbacks.
type RouterEnv struct {
	// AgentRuntime base URL for harness worker-online / run-complete callbacks.
	// Example: https://api.agentruntime.io or http://127.0.0.1:8001
	OrchestratorURL string

	// Control Service base URL for MCP instance connect/disconnect callbacks.
	// Example: https://control.agentruntime.io or http://localhost:8002
	ControlURL string

	// X-Internal-Token for orchestrator and Control callbacks (same platform token).
	InternalToken string

	// Public HTTPS base for self-hosted MCP canonical_url (no path suffix).
	// Example: https://router.agentruntime.io or http://127.0.0.1:8340
	PublicBaseURL string

	// Optional static bearer for WSS connect when not using PAT/JWT.
	AuthToken string
}

// LoadRouterEnv reads relay env vars from the router process environment.
func LoadRouterEnv() RouterEnv {
	env := RouterEnv{
		OrchestratorURL: trimURL(os.Getenv("HARNESS_ORCHESTRATOR_URL")),
		ControlURL:      trimURL(os.Getenv("MCP_CONTROL_SERVER_URL")),
		InternalToken:   strings.TrimSpace(os.Getenv("MCP_CONTROL_INTERNAL_TOKEN")),
		PublicBaseURL:   trimURL(os.Getenv("RELAY_PUBLIC_BASE_URL")),
		AuthToken:       strings.TrimSpace(os.Getenv("RELAY_AUTH_TOKEN")),
	}
	warnDeprecatedRouterEnv()
	return env
}

// ServerConfigFromEnv builds relay ServerConfig from LoadRouterEnv.
func ServerConfigFromEnv() ServerConfig {
	env := LoadRouterEnv()
	cfg := ServerConfig{
		AuthToken:     env.AuthToken,
		PublicBaseURL: env.PublicBaseURL,
	}
	if env.OrchestratorURL != "" {
		cfg.OrchestratorCallback = NewOrchestratorCallback(env.OrchestratorURL, env.InternalToken)
	}
	if env.ControlURL != "" {
		cfg.ControlCallback = NewControlCallback(env.ControlURL, env.InternalToken)
	}
	return cfg
}

func trimURL(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), "/")
}

func warnDeprecatedRouterEnv() {
	deprecated := map[string]string{
		"AGENTRUNTIME_URL":       "HARNESS_ORCHESTRATOR_URL",
		"CONTROL_SERVICE_URL":    "MCP_CONTROL_SERVER_URL",
		"HARNESS_RELAY_URL":      "RELAY_PUBLIC_BASE_URL (on router only; HARNESS_RELAY_URL is for agentruntime dispatch)",
		"AGENTRUNTIME_RELAY_URL": "RELAY_PUBLIC_BASE_URL (router) or agentruntime.relayUrl (workbench WSS)",
	}
	for old, replacement := range deprecated {
		if strings.TrimSpace(os.Getenv(old)) != "" {
			log.Printf("relay: deprecated env %s — use %s instead", old, replacement)
		}
	}
}
