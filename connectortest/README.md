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
| `ValidateDescriptionsNoEllipsis` | Fail when `Description` uses `...` in API paths (SOP §9.2) |
| `AssertStringPropertyMinLength` | Assert one schema property has `minLength` |
| `AssertRequiredStringIDsHaveMinLength` | Fail when a required `*_id` string lacks `minLength=1` |

See [`connectors/docs/BUILD_A_CONNECTOR.md`](../../../connectors/docs/BUILD_A_CONNECTOR.md) §4.7 for the full testing pyramid.
