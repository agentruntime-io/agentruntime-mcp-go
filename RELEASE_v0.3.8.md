# agentruntime-mcp-go v0.3.8

## MCP OAuth 2.1 resource server support

- **`OAuthConfigFromEnv`**, **`OAuthProtectedResourceHandler`**, and **`OAuthMiddleware`** for RFC 9728 protected-resource metadata and JWT bearer auth on MCP routes.
- **`HandlerForAdapter`** and **`RunWithConfig`** wire OAuth middleware when env is configured; PAT-only behavior unchanged when OAuth is disabled.
- **Control config cache** (`fetchControlConfigCached`) with `Retry-After` handling on control server rate limits.

Required for Platform MCP OAuth deploy (`mcp.agentruntime.io`).
