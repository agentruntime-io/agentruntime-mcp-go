package agentruntimemcp

// OAuthScopesSupported lists scopes advertised at /.well-known/oauth-protected-resource
// and expected on access tokens for the full platform MCP catalog (v1 — no user/admin split).
//
// Keep in sync with docs/playbooks/auth/PLATFORM_MCP_OAUTH_DEVOPS_CHECKLIST.md §4.
var OAuthScopesSupported = []string{
	"openid", "profile", "email",
	"mcp:execute",
	"workflow:read", "workflow:run", "workflow:edit",
	"mcp:read", "mcp:write",
	"webhook:manage",
	"vault:read", "vault:write",
	"pat:read",
	"llm:read", "llm:write",
	"analytics:read",
	"billing:read",
	"agent:read", "agent:write",
	"memory:read", "memory:write",
	"chat:read", "chat:write",
	"work:read", "work:write",
}
