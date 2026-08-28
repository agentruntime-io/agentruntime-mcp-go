# agentruntime-mcp-go v0.3.9

## Full Platform MCP OAuth scope catalog

Expands `scopes_supported` on `GET /.well-known/oauth-protected-resource` to match the full Platform MCP tool catalog (no user/admin split for v1).

### Changes

- **`OAuthScopesSupported`** — canonical list of 25 scopes (OIDC + all AgentRuntime MCP PAT scopes) in `oauth_scopes.go`.
- **`OAuthProtectedResourceHandler`** — uses `OAuthScopesSupported` instead of the previous 7-scope hardcoded list.

### Scopes now advertised

`openid`, `profile`, `email`, `mcp:execute`, `workflow:read`, `workflow:run`, `workflow:edit`, `mcp:read`, `mcp:write`, `webhook:manage`, `vault:read`, `vault:write`, `pat:read`, `llm:read`, `llm:write`, `analytics:read`, `billing:read`, `agent:read`, `agent:write`, `memory:read`, `memory:write`, `chat:read`, `chat:write`, `work:read`, `work:write`.

Keep in sync with `docs/playbooks/auth/PLATFORM_MCP_OAUTH_DEVOPS_CHECKLIST.md` §4.

### Upgrade

```bash
go get github.com/agentruntime-io/agentruntime-mcp-go@v0.3.9
```

**Platform MCP:** bump to `v0.3.9`, remove the monorepo `replace` directive in `go.mod`, redeploy prod.

Requires **v0.3.8+** for OAuth middleware (`OAuthConfigFromEnv`, JWT verification, protected-resource endpoint).
