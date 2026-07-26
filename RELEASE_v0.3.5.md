# agentruntime-mcp-go v0.3.5

## Fix: list_tools during sysadmin discover

- **Middleware** — `tools/call` for `list_tools` no longer requires Control tenant config (same as `tools/list`).
- Control discover can fetch hold flags without an MCP instance / API keys.

Required for sysadmin publish review to show **On hold** tools unchecked.
