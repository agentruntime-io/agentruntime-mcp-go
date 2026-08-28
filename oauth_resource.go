package agentruntimemcp

import (
	"encoding/json"
	"net/http"
	"strings"
)

const oauthProtectedResourcePath = "/.well-known/oauth-protected-resource"

// OAuthProtectedResourcePath is the well-known path for RFC 9728 metadata.
const OAuthProtectedResourcePath = oauthProtectedResourcePath

// OAuthProtectedResourceHandler serves GET /.well-known/oauth-protected-resource.
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
	metaURL := strings.TrimRight(cfg.ResourceURL, "/") + oauthProtectedResourcePath
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
