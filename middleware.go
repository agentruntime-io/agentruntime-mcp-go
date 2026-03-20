package agentruntimemcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
)

// methods that only return definitions; config not needed
var noConfigMethods = map[string]bool{
	"tools/list": true,
}

// needsResolvedConfig returns true if the JSON-RPC method requires config (e.g. tools/call).
func needsResolvedConfig(r *http.Request) bool {
	if r.Body == nil {
		return true
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return true
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	var msg struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &msg); err == nil && noConfigMethods[msg.Method] {
		return false
	}
	return true
}

// Middleware wraps an HTTP handler with auth and control config resolution.
// tools/list skips config resolution; config is only fetched for tools/call etc.
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
		needConfig := needsResolvedConfig(r)
		if controlBase != "" && token != "" && needConfig {
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
		} else if controlBase != "" && configRequired && token == "" && needConfig {
			logWarn("missing auth token for control config resolution")
			http.Error(w, "missing auth token for control config resolution", http.StatusUnauthorized)
			return
		}

		r = r.WithContext(WithConfig(r.Context(), cfg))
		next.ServeHTTP(w, r)
	})
}
