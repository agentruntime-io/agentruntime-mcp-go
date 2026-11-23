# agentruntime-mcp-go v0.4.0

## Triggers: connectors declare events, the SDK owns the webhook plumbing

**Breaking release.** The platform now has one trigger engine in control-service. Connectors no longer look up routes,
fan out or sign deliveries to the BFF; they verify the vendor call and hand the event to control. Control, the router
(`connectors/go-connectors`) and the four inbound connectors (`telegram`, `github`, `fathom`, `payments-lk`) move
together. Python and TypeScript SDKs are unchanged (they have no inbound support).

Contract: `docs/usage/TRIGGERS_ENGINE_CONTRACT_2026-11-23.md` (sections 8, 13, 16).

### Added

- **`TriggerAdapter`** (`TriggerManifest() TriggerManifest`), optional like `WebhookAdapter`. `RunWithRouter` serves the
  manifest at `GET /{adapter}/triggers`, requires `X-Internal-Token` (`MCP_CONTROL_INTERNAL_TOKEN`, fails closed when
  unset) and **refuses to start** when the manifest would be refused by control.
- **`TriggerManifest`** and its parts (`TriggerManifestEvent`, `TriggerField`, `TriggerFilterField`, `TriggerDelivery`,
  `TriggerVerify`, `TriggerRegistration`, `TriggerConnection`, `TriggerChannel`): JSON exactly as contract 8.2.
  `Validate(adapter)` applies the control-side rules (known version, unique event ids, filter ops `eq|in|prefix|contains`,
  exact `delivery.path` and `registration.*_path`, valid `scope_schema`, `x-from-events` only on arrays, 64 KB cap);
  `Normalized()` turns null lists into `[]`.
- **`ServeInboundWebhook(InboundWebhookSpec)`** / **`MountInboundWebhook`**: the whole vendor webhook route. It owns the
  route token (`POST /internal/trigger-ingress/resolve`), the body cap (413), a constant-time compare helper
  (`ConstantTimeEqual`), **refusal of an empty vendor secret (401, before `Verify` runs)**, the idempotency key, the post to
  control and the reply. A connector writes only `Verify`, `EventType` and `EventKey` (and `Secret` when the credential is not
  the ingress secret, as for payments-lk). Control or lookup failure answers 502/503 so the vendor retries; unknown or
  disabled routes are 404; duplicates and events no trigger wants are 200.
- **`PostTriggerEvent`** (`POST /internal/trigger-events/connector`, `X-Internal-Token` + `X-Service-Name: connector-router`),
  `NewTriggerEvent` (object body to `payload`, anything else to `payload_raw`), `TriggerEvent`, `TriggerAck`,
  `TriggerPostError`, `ResolveTriggerIngress`, `TriggerIngress`, `TriggerEnv` / `LoadTriggerEnv`.
- **`MountInboundVendorRoutes(mux, InboundVendorSpec)`**: `register-webhook`, `webhook-status` and the new
  **`unregister-webhook`** routes under `/{adapter}/inbound/`, called by **control** (not the BFF). The SDK owns the token
  check, request parsing, the check that `webhook_url` is this adapter's own route for this token, and the response shapes
  (`{ok, webhook_url, registered_scope, vendor_ids, vendor_secret?}`). A connector supplies only the vendor calls.
  `InboundBadRequest` marks a permanent scope error (400); any other error is a retryable 502. Request fields:
  `connection_id`, `server_id`, `scope`, `webhook_url`, `vendor_secret` and the optional `vendor_ids` (what the last
  register returned, echoed by control so a vendor with no list call can still be unregistered). Tenant context stays in
  the `X-Tenant-Id` / `X-Project-Id` headers. `InboundRegisterResult.VendorSecret` is for a vendor that generates the
  signing secret itself (Fathom): control stores it as the ingress secret. `InboundStatusResult.Registered` is a `*bool`:
  nil means the connector cannot tell (control must not read that as stale).
- **`connectortest.InboundWebhookConformance`** and **`FakeControl`**: the suite every inbound connector must pass (no
  credential, wrong credential, empty secret, oversize, replay, unknown token, disabled ingress, control down).
- **`BodyDigest`**, **`InboundUnregisterWebhookPath`**.

### Removed (no compatibility shims)

`DeliverModeB`, `ModeBRequest`, `SignModeB`, `DeliverInboundFanOut`, `LookupInboundAdapterRoute`,
`ResolveInboundForwardTargets`, `InboundAdapterRouteCredentials`, `InboundDeliverTarget`,
`ConnectorWebhookURL`.

### Changed

- `InboundAdapterRouteEnv` / `LoadInboundAdapterRouteEnv` are replaced by **`TriggerEnv` / `LoadTriggerEnv`**
  (`MCP_CONTROL_SERVER_URL`, `MCP_CONTROL_INTERNAL_TOKEN`). `RELAY_PUBLIC_BASE_URL` is no longer read: control passes the
  full public `webhook_url` when it registers. `ResolveMCPConnectionConfig`, `ResolveMCPInstanceConfig` and
  `ValidateInboundInternalToken` take `TriggerEnv`; the token check is now constant time.
- `InboundVendorAdapter` doc: the admin routes are called by control.

### Upgrade

```bash
go get github.com/agentruntime-io/agentruntime-mcp-go@v0.4.0
go mod tidy
```

A connector that used the removed helpers must be ported (see `telegram-connector/mcp` for the pattern): implement
`TriggerManifest()`, build an `InboundWebhookSpec`, and `MountInboundVendorRoutes` if the platform registers the vendor
webhook. Router environment: `MCP_CONTROL_SERVER_URL` and `MCP_CONTROL_INTERNAL_TOKEN` only.
