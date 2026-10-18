# agentruntime-mcp-go v0.3.13

## Inbound adapter route lookup

### Features

- **`adapter_route.go`** — Wheelhouse-backed inbound adapter route lookup for connector ingress.
- **`LoadInboundAdapterRouteEnv`** — reads `GO_WHEELHOUSE_URL`, `GO_WHEELHOUSE_INTERNAL_TOKEN`, `AGENTRUNTIME_BFF_BASE_URL`, `RELAY_PUBLIC_BASE_URL`.
- **`LookupInboundAdapterRoute`** — resolves `route_token` via Wheelhouse internal API.
- **`ResolveInboundForwardTargets`** — picks BFF base URL and signing credentials for adapter forwarding.
- **`ConnectorWebhookPath` / `ConnectorWebhookURL`** — builds public connector webhook URLs.

### Upgrade

```bash
go get github.com/agentruntime-io/agentruntime-mcp-go@v0.3.13
```

Remove any local `replace github.com/agentruntime-io/agentruntime-mcp-go => ...` directives after upgrading.
