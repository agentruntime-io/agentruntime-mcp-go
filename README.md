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

Same `config.yaml` format as Python/TypeScript packages. Env overrides: `HOST`, `PORT`, `MCP_AUTH_MODE`.

## Auth modes

- `none`: no auth
- `token`: `Authorization: Bearer <token>` or `X-MCP-Token`
- `hmac`: headers `X-MCP-KeyId`, `X-MCP-Timestamp`, `X-MCP-Signature`

## Control config resolution

Set `MCP_CONTROL_SERVER_URL`. Config is available in tool handlers via `agentmcp.ConfigFromContext(ctx)`.

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

## Templates

Example MCP server using this SDK:

- [connectors/go-connectors/resend-connector](../../connectors/go-connectors/resend-connector/) – Resend (send_email, list_audiences)
