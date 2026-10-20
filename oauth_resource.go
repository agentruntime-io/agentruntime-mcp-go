package agentruntimemcp

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

const oauthProtectedResourcePath = "/.well-known/oauth-protected-resource"

// OAuthProtectedResourcePath is the well-known path for RFC 9728 metadata.
const OAuthProtectedResourcePath = oauthProtectedResourcePath

// OAuthProtectedResourceMetadataURL returns the RFC 9728 §3.1 metadata URL for a resource:
// the well-known path inserted between the host and the resource's path
// (https://mcp.example.com/mcp → https://mcp.example.com/.well-known/oauth-protected-resource/mcp).
func OAuthProtectedResourceMetadataURL(resourceURL string) string {
	resourceURL = strings.TrimRight(resourceURL, "/")
	u, err := url.Parse(resourceURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return resourceURL + oauthProtectedResourcePath
	}
	return u.Scheme + "://" + u.Host + oauthProtectedResourcePath + strings.TrimRight(u.Path, "/")
}

// OAuthProtectedResourcePaths lists the paths to serve the metadata on: the root form, and the
// RFC 9728 path-inserted form when the resource has a path. Gemini CLI and rmcp clients (Codex, Goose)
// probe the path-inserted form first; Gemini rejects metadata whose resource is not the exact server URL.
func OAuthProtectedResourcePaths(cfg OAuthConfig) []string {
	paths := []string{oauthProtectedResourcePath}
	u, err := url.Parse(strings.TrimRight(cfg.ResourceURL, "/"))
	if err == nil && strings.Trim(u.Path, "/") != "" {
		paths = append(paths, oauthProtectedResourcePath+strings.TrimRight(u.Path, "/"))
	}
	return paths
}

func isOAuthProtectedResourcePath(cfg OAuthConfig, path string) bool {
	for _, p := range OAuthProtectedResourcePaths(cfg) {
		if path == p {
			return true
		}
	}
	return false
}

// OAuthProtectedResourceHandler serves the RFC 9728 metadata (mount it on every OAuthProtectedResourcePaths entry).
func OAuthProtectedResourceHandler(cfg OAuthConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !cfg.Enabled() {
			http.NotFound(w, r)
			return
		}
		payload := map[string]any{
			"resource":                 cfg.ResourceURL,
			"authorization_servers":    cfg.AuthorizationServers,
			"scopes_supported":         OAuthScopesSupported,
			"bearer_methods_supported": []string{"header"},
		}
		if doc := strings.TrimSpace(cfg.ResourceDocumentation); doc != "" {
			payload["resource_documentation"] = doc
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(payload)
	})
}

func writeOAuthChallenge(w http.ResponseWriter, cfg OAuthConfig, scope string) {
	metaURL := OAuthProtectedResourceMetadataURL(cfg.ResourceURL)
	if strings.TrimSpace(scope) == "" && len(cfg.RequiredScopes) > 0 {
		scope = cfg.RequiredScopes[0]
	}
	challenge := "Bearer resource_metadata=\"" + metaURL + "\""
	if scope != "" {
		challenge += ", scope=\"" + scope + "\""
	}
	w.Header().Set("WWW-Authenticate", challenge)
	http.Error(w, "authentication required", http.StatusUnauthorized)
}
