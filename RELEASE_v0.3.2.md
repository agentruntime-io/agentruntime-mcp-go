# agentruntime-mcp-go v0.3.2

## Tool hold

- **`ToolDef`** — registration struct with `Hold bool` (preferred over naked `mcp.Tool`).
- **`AddTool`** — accepts `*ToolDef`, `ToolDef`, or legacy `*mcp.Tool`; optional `WithHold()`.
- **Registry introspection** — `RegisteredTools()`, `HeldToolNames()`, `PublishableToolNames()`, `ResetToolRegistry()` (tests).
- **`connectortest`** — `ParseHeldToolNames`, `PublishableNames`, `ValidateCatalogExcludesHeld`.

Held tools stay on the MCP server in dev; catalog generators should omit them until hold is cleared.

Parity with `agentruntime-mcp@0.3.2` and `@agentruntime-labs/agentruntime-mcp@0.3.2`.
