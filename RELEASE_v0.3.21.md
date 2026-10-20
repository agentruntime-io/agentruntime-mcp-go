# agentruntime-mcp-go v0.3.21

## MCP OAuth: path-aware resource metadata and sign-in on connect

Two changes so common MCP clients can sign in to an OAuth-protected server. Both are no-ops unless the
`AGENTRUNTIME_MCP_RESOURCE_URL` / `AGENTRUNTIME_OAUTH_*` OAuth settings are configured, and the second is opt-in.

### RFC 9728 path-aware protected resource metadata

When the resource URL has a path (`https://mcp.example.com/mcp`), the `WWW-Authenticate` challenge now points at
the path-inserted metadata URL, `https://mcp.example.com/.well-known/oauth-protected-resource/mcp`, and the
middleware serves the metadata at both that path and the root `/.well-known/oauth-protected-resource`.

Servers that mount the metadata handler themselves should mount it on every path from the new
`OAuthProtectedResourcePaths(cfg)`:

```go
for _, path := range agentmcp.OAuthProtectedResourcePaths(oauthCfg) {
	mux.Handle(path, agentmcp.OAuthProtectedResourceHandler(oauthCfg))
}
```

Claude and Gemini CLI require the metadata `resource` to equal the server URL the user enters, path included, so
set `AGENTRUNTIME_MCP_RESOURCE_URL` to the full MCP URL. Keep `AGENTRUNTIME_OAUTH_AUDIENCE` set explicitly: it
defaults to the resource URL.

### `AGENTRUNTIME_OAUTH_REQUIRE_AUTH_ON_CONNECT` (default off)

By default only `tools/call` needs a token; `initialize`, `tools/list` and `ping` stay open. Cursor, Cline,
Continue, Warp and Gemini CLI only start OAuth when the connect handshake returns 401, so they never sign in.
Set `AGENTRUNTIME_OAUTH_REQUIRE_AUTH_ON_CONNECT=true` to return 401 plus the challenge on every unauthenticated
request, including `initialize` and the GET/HEAD discovery probes. `OPTIONS` preflights always pass through, and
PAT bearers are unchanged.

### Upgrade

```bash
go get github.com/agentruntime-io/agentruntime-mcp-go@v0.3.21
go mod tidy
```

Connector servers need no change; behaviour is identical unless OAuth is configured.
