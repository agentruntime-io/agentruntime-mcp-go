# agentruntime-mcp-go v0.3.4 (unreleased)

## Sysadmin hold discovery

- **`list_tools`** introspection tool — returns registered tool names with `hold` flags for Control discover.
- **`PrepareAdapterRegistration`** — resets per-adapter hold registry, runs adapter `Register`, then attaches `list_tools`.
- **`HandlerForAdapter` / `RunWithRegistry`** — use `PrepareAdapterRegistration` so each router adapter exposes hold metadata.

Control discover merges `hold` into sysadmin live tools; publish review unchecked held tools by default.

See `connectors/docs/TOOL_HOLD.md`.
