package agentruntimemcp

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/agentruntime-io/agentruntime-mcp-go/relay"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// HandlerForAdapter creates an http.Handler for a single adapter at the given mount path.
// mountPath is the MCP base path (e.g. "/github/mcp"); schema is at mountPath+"/config/schema".
// Use with a router: mux.Handle(mountPath, HandlerForAdapter(configPath, "github", "/github/mcp"))
func HandlerForAdapter(configPath, adapterName, mountPath string) (http.Handler, error) {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	adapters, err := getAdapters([]string{adapterName})
	if err != nil {
		return nil, err
	}
	server, configSchema := MakeServer(cfg)
	sw := NewSchemaWriter(configSchema)
	for _, a := range adapters {
		PrepareAdapterRegistration(server, sw, a.Register)
	}

	stateless := cfg.Server != nil && cfg.Server.StatelessHTTP
	opts := newStreamableHTTPOptions(stateless)
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, opts)
	handler := Middleware(configSchema, mcpHandler, mountPath)
	handler = ForwardIdentityHeaders(handler)
	handler = ForwardRequestBearer(handler)
	oauthCfg := OAuthConfigFromEnv()
	if oauthCfg.Enabled() {
		handler = OAuthMiddleware(oauthCfg, mountPath, newJWTVerifier(oauthCfg.Issuer, oauthCfg.Audience, oauthCfg.JWKSURL), handler)
	}
	return wrapWithTracing(cfg, handler), nil
}

// RunWithRouter runs the monolith with per-adapter routes: /<adapter>/mcp.
// Each adapter is available at http://host:port/<adapter>/mcp (e.g. /github/mcp, /resend/mcp).
func RunWithRouter(configPath string) error {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return err
	}
	names := ListAdapterNames()
	if len(names) == 0 {
		return &ErrAdapterNotFound{Name: "(no adapters registered)"}
	}

	mux := http.NewServeMux()
	if bridgeHandler, err := HandlerForBridge(configPath); err != nil {
		return err
	} else {
		mux.Handle(BridgeMountPath, bridgeHandler)
		mux.Handle(BridgeMountPath+"/", bridgeHandler)
		log.Printf("MCP bridge route registered at %s", BridgeMountPath)
	}

	if composioHandler, err := HandlerForComposioExecutor(configPath); err != nil {
		return err
	} else {
		mux.Handle(ComposioExecutorMountPath, composioHandler)
		mux.Handle(ComposioExecutorMountPath+"/", composioHandler)
		log.Printf("Composio executor route registered at %s", ComposioExecutorMountPath)
	}

	relayCfg := relay.ServerConfigFromEnv()
	if issuer, audience, jwksURL, ok := relay.UserJWTFromEnv(); ok {
		relayCfg.UserVerifier = newRelayUserVerifier(newJWTVerifier(issuer, audience, jwksURL))
		log.Printf("Relay user-token auth enabled on %s", relay.HarnessConnectPath)
	}
	if relayCfg.OrchestratorCallback != nil {
		log.Printf("Harness orchestrator callbacks enabled")
	}
	if relayCfg.ControlCallback != nil {
		log.Printf("Relay Control callbacks enabled")
	}
	relaySrv := relay.NewServer(relayCfg)
	relaySrv.RegisterRoutes(mux)
	log.Printf("Outbound relay routes registered (%s, %s)", relay.MCPConnectPath, relay.HarnessConnectPath)

	for _, name := range names {
		mountPath := "/" + name + "/mcp"
		h, err := HandlerForAdapter(configPath, name, mountPath)
		if err != nil {
			return err
		}
		mux.Handle(mountPath, h)
		mux.Handle(mountPath+"/", h)

		// Mount the optional routes of adapters that implement WebhookAdapter,
		// InboundVendorAdapter or TriggerAdapter. Adapters that don't are unaffected.
		adps, err := getAdapters([]string{name})
		if err != nil {
			return err
		}
		for _, adp := range adps {
			if err := mountAdapterExtras(mux, name, adp); err != nil {
				return err
			}
		}
	}

	host := "127.0.0.1"
	if cfg.Server != nil && cfg.Server.Host != "" {
		host = cfg.Server.Host
	}
	if h := getEnv("HOST", ""); h != "" {
		host = h
	}
	port := 8000
	if cfg.Server != nil && cfg.Server.Port > 0 {
		port = cfg.Server.Port
	}
	if p := getEnv("PORT", ""); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}
	addr := host + ":" + strconv.Itoa(port)

	log.Printf("MCP router listening on %s (adapters: %v)", addr, names)
	return http.ListenAndServe(addr, mux)
}

// mountAdapterExtras mounts the optional routes an adapter declares by implementing
// WebhookAdapter (the vendor-facing webhook), InboundVendorAdapter (register, status and
// unregister, called by control) and TriggerAdapter (GET /{adapter}/triggers). A trigger
// manifest that control would refuse is an error here, so the router fails at startup instead
// of failing discovery in production.
func mountAdapterExtras(mux *http.ServeMux, name string, adp Adapter) error {
	if wa, ok := adp.(WebhookAdapter); ok {
		wa.RegisterWebhook(mux)
	}
	if iva, ok := adp.(InboundVendorAdapter); ok {
		iva.RegisterInboundVendor(mux)
	}
	if ta, ok := adp.(TriggerAdapter); ok {
		manifest := ta.TriggerManifest()
		if err := manifest.Validate(name); err != nil {
			return fmt.Errorf("adapter %s: %w", name, err)
		}
		mux.Handle(TriggerManifestPath(name), TriggerManifestHandler(manifest, LoadTriggerEnv))
		log.Printf("Trigger manifest route registered at %s", TriggerManifestPath(name))
	}
	return nil
}

// TriggerManifestHandler serves a connector's trigger manifest (GET /{adapter}/triggers).
// It requires the router's internal token and fails closed when none is configured.
func TriggerManifestHandler(manifest TriggerManifest, loadEnv func() TriggerEnv) http.Handler {
	body, err := json.Marshal(manifest.Normalized())
	if loadEnv == nil {
		loadEnv = LoadTriggerEnv
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := ValidateInboundInternalToken(r, loadEnv()); err != nil {
			WriteInboundVendorError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if err != nil {
			WriteInboundVendorError(w, http.StatusInternalServerError, "manifest unavailable")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	})
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
