package agentruntimemcp

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

// Middleware wraps an HTTP handler with auth and control config resolution.
func Middleware(configSchema map[string]any, next http.Handler) http.Handler {
	configRequired := strings.ToLower(os.Getenv("MCP_CONFIG_FETCH_REQUIRED")) != "false"
	controlBase := strings.TrimSpace(os.Getenv("MCP_CONTROL_SERVER_URL"))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSchemaEndpoint(r) {
			logDebug("serving schema endpoint")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(configSchema)
			return
		}

		if err := validateAuth(r); err != nil {
			logWarn("auth failed: %v", err)
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		cfg := ConfigView{}
		token := extractToken(r)
		if controlBase != "" && token != "" {
			ctx := buildRuntimeContext(r)
			resolved, err := fetchControlConfig(token, configSchema, ctx)
			if err != nil {
				logError("control config fetch failed: %v", err)
				if configRequired {
					http.Error(w, "control config resolution failed: "+err.Error(), http.StatusUnauthorized)
					return
				}
			} else if resolved != nil {
				cfg = resolved
				logDebug("control config resolved successfully")
			}
		} else if controlBase != "" && configRequired && token == "" {
			logWarn("missing auth token for control config resolution")
			http.Error(w, "missing auth token for control config resolution", http.StatusUnauthorized)
			return
		}

		r = r.WithContext(WithConfig(r.Context(), cfg))
		next.ServeHTTP(w, r)
	})
}
