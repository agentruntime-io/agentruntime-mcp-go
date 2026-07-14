# toolorg

Tool organization helpers for MCP connectors at **registration / publish** time. See [docs/mcp/MCP_TOOL_ORGANIZATION.md](../../../docs/mcp/MCP_TOOL_ORGANIZATION.md).

**Runtime rule:** catalog and console palette use only stored `mcp_tools.metadata` + sysadmin overlays. Wire-name helpers here do not run at catalog read time.

## Import

```go
import "github.com/agentruntime-io/agentruntime-mcp-go/toolorg"
```

Monorepo local dev (before a tagged release includes `toolorg`):

```go
// go.mod
replace github.com/agentruntime-io/agentruntime-mcp-go => ../../packages/agentruntime-mcp-go
```

## Connector usage (publish payload)

```go
func PublishMetadata(toolName string) map[string]any {
    return toolorg.PublisherMetadata(toolName, toolorg.Metadata{})
}
```

Override specific tools when wire-name suggestion is wrong:

```go
return toolorg.PublisherMetadata(toolName, toolorg.Metadata{
    SuggestedGroup: "docs",
    SuggestedTags:  []string{"docs", "read_only", "v3"},
})
```

Control persists whatever the publish payload includes. Empty metadata stays empty.

## API summary

| Function | Purpose |
|----------|---------|
| `SuggestFromWireName` | Suggest group + tags when **building** publish metadata |
| `PublisherMetadata` | Build `mcp_tools.metadata` map for publish |
| `DefaultPublisherMetadata` | `PublisherMetadata(name, Metadata{})` |
| `FormatDisplayName` | `clickup_get_task` → `Get task` |
| `GroupLabel` | `tasks` → `Tasks` |
| `ParseMetadata` | Unmarshal published JSONB |
| `MergeEffective` | Published + tenant overlay → effective org (no wire inference) |
| `MetadataIsEmpty` | Whether publish payload omitted organization fields |

## Tests

Parity fixtures in `fixtures/suggest_cases.json` cover `SuggestFromWireName` / `PublisherMetadata` at authoring time.

```bash
go test ./toolorg/...
```
