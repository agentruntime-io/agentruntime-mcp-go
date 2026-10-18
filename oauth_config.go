package agentruntimemcp

import (
	"os"
	"strings"
)

// OAuthConfig holds optional MCP OAuth 2.1 resource-server settings.
// When Enabled() is false, OAuth middleware is a no-op (PAT-only behavior).
type OAuthConfig struct {
	ResourceURL            string
	Issuer                 string
	JWKSURL                string
	Audience               string
	RequiredScopes         []string
	AuthorizationServers   []string
	ResourceDocumentation  string
}

const (
	envMCPResourceURL           = "AGENTRUNTIME_MCP_RESOURCE_URL"
	envOAuthIssuer              = "AGENTRUNTIME_OAUTH_ISSUER"
	envOAuthJWKSURL             = "AGENTRUNTIME_OAUTH_JWKS_URL"
	envOAuthAudience            = "AGENTRUNTIME_OAUTH_AUDIENCE"
	envOAuthRequiredScopes      = "AGENTRUNTIME_OAUTH_REQUIRED_SCOPES"
	envOAuthAuthorizationServer = "AGENTRUNTIME_OAUTH_AUTHORIZATION_SERVER"
	envOAuthResourceDocs        = "AGENTRUNTIME_OAUTH_RESOURCE_DOCUMENTATION"
)

// OAuthConfigFromEnv loads MCP OAuth settings from environment variables.
func OAuthConfigFromEnv() OAuthConfig {
	resource := strings.TrimSpace(os.Getenv(envMCPResourceURL))
	issuer := strings.TrimSpace(os.Getenv(envOAuthIssuer))
	jwks := strings.TrimSpace(os.Getenv(envOAuthJWKSURL))
	audience := strings.TrimSpace(os.Getenv(envOAuthAudience))
	if audience == "" {
		audience = resource
	}
	authServer := strings.TrimSpace(os.Getenv(envOAuthAuthorizationServer))
	if authServer == "" {
		authServer = issuer
	}
	var authServers []string
	if authServer != "" {
		authServers = []string{strings.TrimRight(authServer, "/")}
	}
	required := splitScopeList(os.Getenv(envOAuthRequiredScopes))
	if len(required) == 0 {
		required = []string{"mcp:execute"}
	}
	return OAuthConfig{
		ResourceURL:           strings.TrimRight(resource, "/"),
		Issuer:                strings.TrimRight(issuer, "/"),
		JWKSURL:               jwks,
		Audience:              audience,
		RequiredScopes:        required,
		AuthorizationServers:  authServers,
		ResourceDocumentation: strings.TrimSpace(os.Getenv(envOAuthResourceDocs)),
	}
}

// Enabled reports whether OAuth resource-server mode is configured.
func (c OAuthConfig) Enabled() bool {
	return strings.TrimSpace(c.ResourceURL) != "" &&
		strings.TrimSpace(c.Issuer) != "" &&
		strings.TrimSpace(c.JWKSURL) != "" &&
		strings.TrimSpace(c.Audience) != ""
}

func splitScopeList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
