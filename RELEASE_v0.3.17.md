# agentruntime-mcp-go v0.3.17

## Inbound adapter route credentials — MCP instance scope

Extends `InboundAdapterRouteCredentials` (Control internal lookup / `LookupInboundAdapterRoute`) with optional fields returned from Wheelhouse:

- **`mcp_instance_id`** — linked MCP instance on the adapter route row
- **`project_id`** — subscription project scope for resolve-test

### Why

Some vendors sign webhooks with credentials stored on the tenant MCP instance, not the platform `vendor_secret`. Example: **Payments.lk** `Payments-Signature` uses the merchant `sk_test_` / `sk_live_` from connector config. At delivery time the connector must call `ResolveMCPInstanceConfig`, which requires instance + project + tenant — not only route token lookup.

GitHub/Fathom/Telegram continue to verify with `vendor_secret`; they ignore these fields.

### Upgrade

```bash
go get github.com/agentruntime-io/agentruntime-mcp-go@v0.3.17
```

Required for **payments-lk** inbound webhook verification. Wheelhouse and Control must also return the new JSON fields on adapter route credential lookup (platform release alongside this tag).
