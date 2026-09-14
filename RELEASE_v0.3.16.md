# agentruntime-mcp-go v0.3.16

## Inbound vendor registration — connector-owned admin routes

Connectors that implement `InboundVendorAdapter` expose platform-managed vendor webhook registration on the connector router. BFF proxies register/status; vendor HTTP stays in each connector.

### API

- **`InboundVendorAdapter`** — optional interface; `RegisterInboundVendor(mux)` mounts admin routes.
- **`InboundRegisterWebhookPath` / `InboundWebhookStatusPath`** — `POST /{adapter}/inbound/register-webhook/{route_token}`, `GET …/webhook-status/{route_token}`.
- **`ResolveMCPInstanceConfig`** — Control resolve-test from connector router (`X-Service-Name: connector-router`).
- **`ValidateInboundInternalToken`**, **`InboundVendorContextFromRequest`**, **`InboundRouteToken`**, JSON helpers.

Requires `go-authz` delegation allowing `connector-router` on resolve-test.

### Upgrade

```bash
go get github.com/agentruntime-io/agentruntime-mcp-go@v0.3.16
```

Implement `RegisterInboundVendor` in telegram/github/fathom (or your adapter) and deploy an updated connector router before relying on Console register/status.
