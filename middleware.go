package agentruntimemcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
)

// methods that only return definitions or session setup; config not needed
var noConfigMethods = map[string]bool{
	"tools/list":                true,
	"initialize":                true,
	"notifications/initialized": true, // MCP handshake; config only needed for tools/call
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
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &msg); err != nil {
		return true
	}
	if noConfigMethods[msg.Method] {
		return false
	}
	return true
}

// Middleware wraps an HTTP handler with Control config resolution when the adapter registered config keys.
// Bearer / X-MCP-Token carries the run token for POST /mcp/config. No static ingress token env.
// mountPath is the MCP base path (e.g. "/mcp" or "/github/mcp") for schema endpoint detection; use "" for default "/mcp".
//
// Config resolution order for tools/call (and other methods that need config):
//  1. Resolve Control when URL + token are present (subject to MCP_CONFIG_FETCH_REQUIRED for hard failures).
//  2. Merge environment variables for each schema key (case-insensitive names: key or AR_<key>).
//     Env wins on key clash with Control.
//  3. If Control fails or is skipped but env supplies all required keys, the request still proceeds.
func Middleware(configSchema map[string]any, next http.Handler, mountPath string) http.Handler {
	if mountPath == "" {
		mountPath = "/mcp"
	}
	configRequired := strings.ToLower(os.Getenv("MCP_CONFIG_FETCH_REQUIRED")) != "false"
	controlBase := strings.TrimSpace(os.Getenv("MCP_CONTROL_SERVER_URL"))
	wantControl := configSchemaHasKeys(configSchema)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSchemaEndpointForMount(r, mountPath) {
			logDebug("serving schema endpoint")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(configSchema)
			return
		}

		cfg := ConfigView{}
		token := extractToken(r)
		needConfig := needsResolvedConfig(r)

		envIdx := newLowercaseEnvIndex()
		envKeys := envOverridesFromSchema(configSchema, envIdx)

		if wantControl && needConfig {
			tryControl := controlBase != "" && token != ""

			if tryControl {
				ctx := buildRuntimeContext(r)
				logRuntimeContextSummary(r, ctx, needConfig)
				resolved, err := fetchControlConfig(token, configSchema, ctx)
				if err != nil {
					logError("control config fetch failed: %v", err)
					if configRequired && !envAloneSatisfiesRequired(configSchema, envIdx) {
						status := http.StatusBadGateway
						clientMsg := "control config resolution failed: " + err.Error()
						var ce *ControlError
						if errors.As(err, &ce) {
							status = ce.Status
							if hm := HumanMessageFromControlAPIBody(ce.Body); hm != "" {
								clientMsg = "MCP control config: " + hm
							}
							if status == http.StatusUnauthorized || status == http.StatusForbidden {
								status = http.StatusUnprocessableEntity
							}
						}
						if status < 400 || status >= 600 {
							status = http.StatusBadGateway
						}
						http.Error(w, clientMsg, status)
						return
					}
				} else if resolved != nil {
					cfg = resolved
					logDebug("control config resolved successfully")
				}
			} else {
				if controlBase == "" && configRequired && !envAloneSatisfiesRequired(configSchema, envIdx) {
					logWarn("MCP_CONTROL_SERVER_URL is required when the adapter registers config schema keys")
					http.Error(w, "MCP_CONTROL_SERVER_URL is required when config schema has keys", http.StatusServiceUnavailable)
					return
				}
				if controlBase != "" && token == "" && configRequired && !envAloneSatisfiesRequired(configSchema, envIdx) {
					logWarn("missing auth token for control config resolution")
					http.Error(w, "missing auth token for control config resolution", http.StatusUnauthorized)
					return
				}
			}
		}

		finalCfg := mergeControlWithEnvPriority(cfg, envKeys, configSchema)
		r = r.WithContext(WithConfig(r.Context(), finalCfg))
		next.ServeHTTP(w, r)
	})
}
