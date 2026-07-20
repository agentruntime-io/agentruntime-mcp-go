# toolschema



Builds MCP tool JSON Schemas from Go input/output structs with optional field constraints.



## Tags



| Tag | Purpose |

|-----|---------|

| `json` | Field name + requiredness (`omitempty` = optional) — same as jsonschema-go |

| `jsonschema` | Human description only — same as jsonschema-go |

| `agentschema` | Machine constraints (all optional) |



### `agentschema` keys



| Key | Applies to | Example |

|-----|------------|---------|

| `minLength` | string | `minLength=1` |

| `maxLength` | string | `maxLength=1024` |

| `minimum` | integer / number | `minimum=1` |

| `maximum` | integer / number | `maximum=100` |

| `pattern` | string (regex) | `pattern=^[a-zA-Z0-9_-]+$` |

| `format` | string (metadata) | `format=email` |

| `enum` | string (comma-separated) | `enum=opt_in,opt_out` |

| `default` | explicit default when arg omitted | `default=30`, `default=contacts.csv` |



Combine with commas: `agentschema:"minLength=1,maxLength=64"`.



The `jsonschema` tag cannot use `key=value` syntax (reserved by google/jsonschema-go). Use **`agentschema`** for constraints instead.



## Example



```go

type GetTaskInput struct {

    TaskID string `json:"task_id" jsonschema:"ClickUp task ID" agentschema:"minLength=1"`

    Name   string `json:"name" jsonschema:"Task title" agentschema:"minLength=1,maxLength=1024"`

    Limit  int    `json:"limit,omitempty" jsonschema:"Page size; default 30" agentschema:"minimum=1,maximum=100"`

    Owner  string `json:"owner" jsonschema:"GitHub org or user" agentschema:"pattern=^[a-zA-Z0-9_-]+$"`

}

```



Register with `agentmcp.AddTool` (infers schema + agentschema) or set `Tool.InputSchema` explicitly:



```go

schema, _ := toolschema.For[GetTaskInput](nil)

mcp.AddTool(server, &mcp.Tool{Name: "clickup_get_task", InputSchema: schema}, handler)

```



## Enforcement



Constraints appear in published `inputSchema` and are enforced by:



- MCP go-sdk `applySchema` on direct tool calls

- AgentRuntime `ValidateArgs` at workflow runtime

- Workflow dry-run `validateMCPRequiredToolArgs`



Use `minLength=1` on required IDs when empty string should fail. Do **not** set it on optional update fields where omission means “no change”.



`format` is emitted in schema for documentation/clients; platform validation focuses on length, numeric bounds, and `pattern`.

**`enum` and `default`** are supported in `agentschema`. Enum values are comma-separated (`enum=opt_in,opt_out`). Defaults are explicit only (`default=30`); Go zero values are never inferred automatically. See [TOOL_ARG_VALIDATION.md](../../../connectors/docs/TOOL_ARG_VALIDATION.md).

