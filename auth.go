package agentruntimemcp

import (
	"net/http"
	"strings"
)

func extractToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	if t := r.Header.Get("X-MCP-Token"); t != "" {
		return strings.TrimSpace(t)
	}
	return ""
}

func isSchemaEndpoint(r *http.Request) bool {
	return isSchemaEndpointForMount(r, "/mcp")
}

// isSchemaEndpointForMount returns true if the request is for the config schema.
// mountPath is the MCP base path (e.g. "/mcp" or "/github/mcp"); schema is at mountPath+"/config/schema".
func isSchemaEndpointForMount(r *http.Request, mountPath string) bool {
	if r.Method != http.MethodGet {
		return false
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	schemaPath := strings.TrimSuffix(mountPath, "/") + "/config/schema"
	return path == schemaPath
}
