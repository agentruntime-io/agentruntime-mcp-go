# agentruntime-mcp-go v0.3.6

## Remove list_tools; omit held tools from tools/list

- **Removed `list_tools`** introspection tool — no operator tool on the public MCP wire.
- **`Hold: true` tools** are recorded for CI/catalog but **not** registered on the MCP server (omitted from `tools/list`).
- **`PrepareAdapterRegistration`** only resets registry and runs adapter `Register` (no introspection attach).

Sysadmin discover reads publishable tools directly from standard `tools/list`.
