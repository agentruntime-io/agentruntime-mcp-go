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
	// Also the token AgentRuntime must present to dispatch a harness job or pick a worker.
	InternalToken string

	// Public HTTPS base for self-hosted MCP canonical_url (no path suffix).
	// Example: https://router.agentruntime.io or http://127.0.0.1:8340
	PublicBaseURL string

	// Optional static bearer for WSS connect (dev). The tenant and user of such a
	// connection are whatever the client claims.
	AuthToken string

	// BFF base URL that validates a user's Keycloak access token on WSS connect
	// (GET /v1/me). Example: https://api.agentruntime.io
	AuthBFFURL string

	// Wheelhouse base URL and internal token that validate pat_ bearers on WSS
	// connect (POST /internal/pats/introspect).
	AuthWheelhouseURL   string
	AuthWheelhouseToken string

	// AllowInsecure turns relay authentication off. Local development only.
	AllowInsecure bool

	// AllowLegacyDispatch accepts dispatch / worker-pick requests without
	// X-Internal-Token or X-Tenant-Id. Rollout window only; remove once AgentRuntime sends them.
	AllowLegacyDispatch bool
}

// LoadRouterEnv reads relay env vars from the router process environment.
func LoadRouterEnv() RouterEnv {
	env := RouterEnv{
		OrchestratorURL:     trimURL(os.Getenv("HARNESS_ORCHESTRATOR_URL")),
		ControlURL:          trimURL(os.Getenv("MCP_CONTROL_SERVER_URL")),
		InternalToken:       strings.TrimSpace(os.Getenv("MCP_CONTROL_INTERNAL_TOKEN")),
		PublicBaseURL:       trimURL(os.Getenv("RELAY_PUBLIC_BASE_URL")),
		AuthToken:           strings.TrimSpace(os.Getenv("RELAY_AUTH_TOKEN")),
		AuthBFFURL:          trimURL(os.Getenv("RELAY_AUTH_BFF_URL")),
		AuthWheelhouseURL:   trimURL(os.Getenv("RELAY_AUTH_WHEELHOUSE_URL")),
		AuthWheelhouseToken: strings.TrimSpace(os.Getenv("RELAY_AUTH_WHEELHOUSE_INTERNAL_TOKEN")),
		AllowInsecure:       envTruthy(os.Getenv("RELAY_ALLOW_INSECURE")),
		AllowLegacyDispatch: envTruthy(os.Getenv("RELAY_ALLOW_LEGACY_DISPATCH")),
	}
	warnDeprecatedRouterEnv()
	return env
}

// ServerConfigFromEnv builds relay ServerConfig from LoadRouterEnv.
func ServerConfigFromEnv() ServerConfig {
	env := LoadRouterEnv()
	cfg := ServerConfig{
		AuthToken:           env.AuthToken,
		InternalToken:       env.InternalToken,
		AllowInsecure:       env.AllowInsecure,
		AllowLegacyDispatch: env.AllowLegacyDispatch,
		PublicBaseURL:       env.PublicBaseURL,
		Verifier: NewVerifier(VerifierConfig{
			BFFURL:          env.AuthBFFURL,
			WheelhouseURL:   env.AuthWheelhouseURL,
			WheelhouseToken: env.AuthWheelhouseToken,
		}),
	}
	if env.OrchestratorURL != "" {
		cfg.OrchestratorCallback = NewOrchestratorCallback(env.OrchestratorURL, env.InternalToken)
	}
	if env.ControlURL != "" {
		cfg.ControlCallback = NewControlCallback(env.ControlURL, env.InternalToken)
	}
	return cfg
}

func envTruthy(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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
