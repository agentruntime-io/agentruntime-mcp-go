package agentruntimemcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// OAuthMiddleware enforces MCP OAuth 2.1 on tool execution when OAuthConfig is enabled.
// Handshake methods (initialize, tools/list) remain unauthenticated per Strategy A in PLATFORM_MCP_OAUTH.md.
func OAuthMiddleware(cfg OAuthConfig, mountPath string, verifier *jwtVerifier, next http.Handler) http.Handler {
	if !cfg.Enabled() {
		return next
	}
	if mountPath == "" {
		mountPath = "/mcp"
	}
	mountPath = strings.TrimSuffix(mountPath, "/")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimSuffix(r.URL.Path, "/")
		if path == oauthProtectedResourcePath {
			OAuthProtectedResourceHandler(cfg).ServeHTTP(w, r)
			return
		}

		if !strings.HasPrefix(path, mountPath) {
			next.ServeHTTP(w, r)
			return
		}

		if !mcpMethodRequiresAuth(r) {
			next.ServeHTTP(w, r)
			return
		}

		token := extractToken(r)
		if token == "" {
			writeOAuthChallenge(w, cfg, "")
			return
		}
		if strings.HasPrefix(token, "pat_") {
			next.ServeHTTP(w, r)
			return
		}
		if !looksLikeJWT(token) {
			writeOAuthChallenge(w, cfg, "")
			return
		}

		claims, err := verifier.Verify(r.Context(), token)
		if err != nil {
			writeOAuthChallenge(w, cfg, "")
			return
		}
		if !scopesSatisfied(claimScopes(claims), cfg.RequiredScopes) {
			writeOAuthChallenge(w, cfg, strings.Join(cfg.RequiredScopes, " "))
			return
		}

		next.ServeHTTP(w, r)
	})
}

func mcpMethodRequiresAuth(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
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
	if err := json.Unmarshal(body, &msg); err != nil {
		return true
	}
	method := strings.TrimSpace(msg.Method)
	switch method {
	case "tools/list", "initialize", "notifications/initialized", "ping":
		return false
	default:
		return true
	}
}
