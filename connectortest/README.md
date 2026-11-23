# connectortest

CI-friendly helpers for MCP connector `mcp/register.go` catalogs.

Use in each connector's `mcp/register_test.go` (no network, runs on every `go test`).

## Usage

```go
package mcp

import (
    "os"
    "testing"
    "github.com/agentruntime-io/agentruntime-mcp-go/connectortest"
)

func TestRegister_wireNames(t *testing.T) {
    raw, _ := os.ReadFile("register.go")
    names := connectortest.ParseRegisterToolNames(string(raw))
    if err := connectortest.ValidateWireNames(names, connectortest.WireNamePolicy{
        VendorPrefix:    "clickup",
        AllowedTopLevel: []string{"clickup_search"},
        ForbiddenExact:  []string{"clickup_get_workspace_hierarchy"},
    }); err != nil {
        t.Fatal(err)
    }
}

func TestRegister_descriptionsNoEllipsis(t *testing.T) {
    raw, _ := os.ReadFile("register.go")
    if err := connectortest.ValidateDescriptionsNoEllipsis(string(raw)); err != nil {
        t.Fatal(err)
    }
}
```

## API

| Function | Purpose |
|----------|---------|
| `ParseRegisterToolNames` | Extract wire names from `register.go` source |
| `ValidateWireNames` | Vendor prefix, resource segment, top-level allowlist, forbidden removed tools |
| `ValidateToolCount` | Assert tool count in range (catch accidental add/remove) |
| `ParseHeldToolNames` | Extract wires with `Hold: true` or `WithHold()` from `register.go` |
| `PublishableNames` | Registered names minus held |
| `ValidateCatalogExcludesHeld` | Fail when a held wire appears in catalog JSON names |
| `ValidateDescriptionsNoEllipsis` | Fail when `Description` uses `...` in API paths (SOP §9.2) |
| `AssertStringPropertyMinLength` | Assert one schema property has `minLength` |
| `AssertRequiredStringIDsHaveMinLength` | Fail when a required `*_id` string lacks `minLength=1` |

## Inbound webhook conformance

A connector that receives vendor webhooks must pass `InboundWebhookConformance`. It runs the connector's
real handler against a fake control-service (`FakeControl`) and checks: a valid delivery is posted with the
right `event_type`, `event_key` and untouched payload; **no credential**, a **wrong credential** and an
**empty vendor secret** (even with a matching empty credential, which is a valid HMAC) are refused with 401
and nothing is posted; an **oversize** body is refused with 413; a **replay** carries the same event key; an
**unknown** route token and a **disabled** ingress are 404; other methods are 405; control down on lookup is
503 and on post is 502 (the vendor retries).

```go
func TestWebhook_conformance(t *testing.T) {
    mux := http.NewServeMux()
    adapter{}.RegisterWebhook(mux)
    connectortest.InboundWebhookConformance{
        Adapter:      "github",
        Handler:      mux,
        VendorSecret: "whsec-test",
        Body:         []byte(`{"ref":"refs/heads/main","repository":{"full_name":"o/r"}}`),
        Prepare:      func(r *http.Request, body []byte) { r.Header.Set("X-GitHub-Event", "push"); r.Header.Set("X-GitHub-Delivery", "d-1") },
        Sign:         func(r *http.Request, body []byte, secret string) { /* set the vendor signature header */ },
        WantEventType: "push",
        WantEventKey:  "github-d-1",
    }.Run(t)
}
```

`ControlRoutes` adds control endpoints the connector calls while verifying (payments-lk reads the merchant key
through `/v1/mcp/config/resolve-test`). `TriggerManifest.Validate(adapter)` in the root package checks a manifest
the way control does; call it from the connector's tests too.

See [`connectors/docs/BUILD_A_CONNECTOR.md`](../../../connectors/docs/BUILD_A_CONNECTOR.md) §4.7 for the full testing pyramid.
