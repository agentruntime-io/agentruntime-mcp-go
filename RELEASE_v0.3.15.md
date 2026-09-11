# agentruntime-mcp-go v0.3.15

## Inbound adapter route lookup — Control facade

### Breaking change (v0.3.14 → v0.3.15)

Adapter ingress lookup no longer calls Wheelhouse directly. Connectors and the go-connectors router must use Control internal API and the same fleet env as relay callbacks.

| v0.3.14 (removed) | v0.3.15 (use instead) |
|-------------------|------------------------|
| `GO_WHEELHOUSE_URL` | `MCP_CONTROL_SERVER_URL` |
| `GO_WHEELHOUSE_INTERNAL_TOKEN` | `MCP_CONTROL_INTERNAL_TOKEN` |
| `AGENTRUNTIME_BFF_BASE_URL` | *(returned as `bff_base_url` from Control lookup)* |
| Wheelhouse `GET …/adapter-routes/{token}/credentials` | Control `GET /internal/mcp/inbound-adapter-routes/{token}` |

Requires Control `[inbound_webhooks] BFF_BASE_URL` and handler shipped in control-service.

### API

- **`LoadInboundAdapterRouteEnv`** — reads `MCP_CONTROL_SERVER_URL`, `MCP_CONTROL_INTERNAL_TOKEN`, `RELAY_PUBLIC_BASE_URL`.
- **`LookupInboundAdapterRoute`** — resolves `route_token` via Control (returns `bff_base_url`).
- **`ResolveInboundForwardTargets`** — uses `creds.BFFBaseURL` from lookup response.
- **`ConnectorWebhookPath` / `ConnectorWebhookURL`** — unchanged.

### Upgrade

```bash
go get github.com/agentruntime-io/agentruntime-mcp-go@v0.3.15
```

Remove any local `replace github.com/agentruntime-io/agentruntime-mcp-go => ...` directives after upgrading.

Deploy Control with inbound adapter route handler before upgrading connectors/router.
