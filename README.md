# AgentRuntime MCP SDK (Go)

Opinionated SDK for building MCP servers with the [official modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk).

## Install

```bash
go get github.com/agentruntime-io/agentruntime-mcp-go
```

## Minimal example

```go
package main

import (
	"context"
	"fmt"

	agentmcp "github.com/agentruntime-io/agentruntime-mcp-go"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AddInput struct {
	A float64 `json:"a" jsonschema:"required"`
	B float64 `json:"b" jsonschema:"required"`
}

type AddOutput struct {
	Result     float64 `json:"result"`
	Expression string  `json:"expression"`
}

func add(ctx context.Context, req *mcp.CallToolRequest, in AddInput) (*mcp.CallToolResult, AddOutput, error) {
	cfg := agentmcp.ConfigFromContext(ctx)
	// cfg["region"] etc. - resolved from control server
	return nil, AddOutput{
		Result:     in.A + in.B,
		Expression: fmt.Sprintf("%g + %g", in.A, in.B),
	}, nil
}

func main() {
	agentmcp.Run("config.yaml", func(server *mcp.Server, _ map[string]any) {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "add",
			Description: "Add two numbers",
		}, add)
	})
}
```

## Config

Same `config.yaml` format as Python/TypeScript packages. Env overrides: `HOST`, `PORT`, and Control-related vars (see [docs/mcp/mcp_env.md](../../docs/mcp/mcp_env.md)).

## Control integration

- **Registration** — If your adapter adds **no** config fields via `SchemaWriter` / `WriteSchema`, the merged schema is empty and the middleware **skips** `POST /mcp/config` for that adapter (including in the router: each `/<adapter>/mcp` has its own schema).
- **`MCP_CONTROL_SERVER_URL`** — Control base URL. Required (with a run token) when the adapter has **non-empty** schema and **`MCP_CONFIG_FETCH_REQUIRED`** is not `false`.
- **`Authorization: Bearer`** / **`X-MCP-Token`** — run token for Control (not a static env secret).

Local development for a **secretful** adapter without Control: set **`MCP_CONFIG_FETCH_REQUIRED=false`** or provide a dev Control URL. No-config adapters need no Control env.

## Proxy (library)

```go
import agentmcp "github.com/agentruntime-io/agentruntime-mcp-go"

agentmcp.RunProxy(
    "http://127.0.0.1:8000/mcp",  // target URL
    "tools.yaml",                  // overlay file (optional, use "" for none)
    "",                            // bearer token (optional)
    "127.0.0.1",
    8010,
)
```

## Adapter pattern (plugin / monolith)

Like foodics/worker-go: adapters register in `init()`, load via blank imports.

```go
// In connector/mcp/register.go
func init() {
    agentmcp.RegisterAdapter("resend", func(input agentmcp.AdapterConstructorInput) (agentmcp.Adapter, error) {
        return &adapter{}, nil
    })
}

// Monolith: all registered adapters
import _ "github.com/.../resend-connector-go/mcp"
agentmcp.RunWithRegistry("config.yaml")

// Individual: only one adapter
agentmcp.RunWithRegistry("config.yaml", "resend")
```

## Templates

Example MCP server using this SDK:

- [connectors/go-connectors/resend-connector](../../connectors/go-connectors/resend-connector/) – Resend (send_email, list_audiences)
