# agentruntime-mcp-go v0.3.11

## Composio executor + relay env cleanup

### Features

- **Composio executor** — `HandlerForComposioExecutor` at `/composio/mcp` (Option B REST). Fetches toolkit metadata from Control (`composio` payload), serves `tools/list` and `tools/call` via Composio API v3.1.
- **Control payload** — `ControlPayload.Composio` decodes the Control `/mcp/config` composio block (toolkit, allowlist, published_tools).
- **Relay env** — `relay/env.go` centralizes router env (`LoadRouterEnv`, `ServerConfigFromEnv`). Canonical names only; deprecated vars logged and ignored.

### Router

- Registers Composio executor route alongside bridge and relay.
- Relay callbacks configured via `ServerConfigFromEnv()` instead of inline getenv.

### Upgrade

```bash
go get github.com/agentruntime-io/agentruntime-mcp-go@v0.3.11
```

**go-connectors router:** bump dependency and redeploy. Ensure Control returns `composio` in `/mcp/config` for Composio catalog instances.
