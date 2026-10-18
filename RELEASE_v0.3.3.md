# agentruntime-mcp-go v0.3.3

## Tool schema: `enum` and `default`

- **`toolschema`** — `agentschema` tag supports `enum=opt_in,opt_out` (comma-separated values) and `default=30` / `default=contacts.csv` (explicit only; Go zero values are never inferred).
- Comma-separated enum values merge correctly with other `agentschema` keys on the same tag.

Parity with `agentruntime-mcp@0.3.3` and `@agentruntime-labs/agentruntime-mcp@0.3.3`.

See monorepo `connectors/docs/TOOL_ARG_VALIDATION.md` for authoring guidance.
