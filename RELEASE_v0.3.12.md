# agentruntime-mcp-go v0.3.12

## Composio MCP handshake fix

### Fixes

- **Composio executor** — Handle MCP `initialize`, `notifications/initialized`, and `ping` before Control config / Composio metadata resolution so streamable MCP clients can connect.
- **Default tool args** — `applyComposioDefaultToolArguments` injects `user_id: "me"` when omitted on `tools/call`.

### Upgrade

```bash
go get github.com/agentruntime-io/agentruntime-mcp-go@v0.3.12
```

**go-connectors router:** bump dependency and redeploy so `https://<router>/composio/mcp` serves the fixed handshake. Pair with Control `enrichMCPConfigServerID` so `/mcp/config` includes the `composio` block when only `instance_id` is sent.
