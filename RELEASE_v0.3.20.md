# agentruntime-mcp-go v0.3.20

## Tool annotations reach tools/list

`AddTool` used to drop a tool's MCP `Annotations` before it reached `tools/list`, so `readOnlyHint` and the
other hints never left the connector. They now pass through, and `ToolDef` has an `Annotations` field for
connectors that register tools that way.

The platform marks a connector tool read-only only when its server sets `annotations.readOnlyHint`: the agent
form's **Read-only** button and surface `connection.read` binds rely on it.

### Upgrade

```bash
go get github.com/agentruntime-io/agentruntime-mcp-go@v0.3.20
go mod tidy
```

From `connectors/go-connectors`, run the README `foreach` (or bulk script) so every adapter module pins **v0.3.20**.
