# agentruntime-mcp-go v0.3.8

## MCP OAuth 2.1 resource server

Adds optional OAuth resource-server support for Platform MCP (`mcp.agentruntime.io`) and any adapter built on `HandlerForAdapter` / `RunWithConfig`.

### New APIs

- **`OAuthConfigFromEnv()`** — load OAuth settings from environment variables.
- **`OAuthProtectedResourceHandler()`** — serve RFC 9728 metadata at `GET /.well-known/oauth-protected-resource`.
- **`OAuthMiddleware()`** — enforce JWT bearer auth on authenticated MCP methods (`tools/call`, etc.).
- **`newJWTVerifier()`** (internal) — JWKS-backed JWT validation with issuer/audience/scope checks.

### Behavior

- **Strategy A (handshake open):** `initialize` and `tools/list` stay unauthenticated; tool execution requires a valid OAuth access token when OAuth is enabled.
- **PAT unchanged:** `Authorization: Bearer pat_…` continues to work alongside OAuth JWTs.
- **Opt-in:** when OAuth env vars are unset, middleware is a no-op — existing PAT-only servers behave exactly as before.
- **Auto-wiring:** `HandlerForAdapter` and `RunWithConfig` attach OAuth middleware when `OAuthConfig.Enabled()` is true.

### Environment variables

| Variable | Purpose |
|----------|---------|
| `AGENTRUNTIME_MCP_RESOURCE_URL` | MCP resource identifier (e.g. `https://mcp.agentruntime.io`) |
| `AGENTRUNTIME_OAUTH_ISSUER` | OIDC issuer |
| `AGENTRUNTIME_OAUTH_JWKS_URL` | JWKS endpoint for JWT verification |
| `AGENTRUNTIME_OAUTH_AUDIENCE` | Expected `aud` claim (defaults to resource URL) |
| `AGENTRUNTIME_OAUTH_REQUIRED_SCOPES` | Comma-separated scopes (default `mcp:execute`) |
| `AGENTRUNTIME_OAUTH_AUTHORIZATION_SERVER` | Authorization server URL in protected-resource metadata |
| `AGENTRUNTIME_OAUTH_RESOURCE_DOCUMENTATION` | Optional docs URL in metadata |

### Control config cache

- **`fetchControlConfigCached`** — TTL cache (30–120s, env-tunable) with `singleflight` deduplication.
- **`Retry-After` support** — `ControlError` now carries `RetryAfterSec`; middleware respects rate-limit responses from Control.

## Upgrade

```bash
go get github.com/agentruntime-io/agentruntime-mcp-go@v0.3.8
```

**Platform MCP** (`agentruntime-platform-mcp`) requires this release for OAuth deploys. Remove any local `replace` directive in `go.mod` for production/CI builds.

## Consumers

- `agentruntime-platform-mcp` — OAuth protected-resource endpoint + JWT middleware on `/mcp`
- Go connectors — no wire changes required; PAT-only behavior unchanged unless OAuth env is set
