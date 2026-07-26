package agentruntimemcp

import "github.com/modelcontextprotocol/go-sdk/mcp"

// PrepareAdapterRegistration clears the per-adapter hold registry and runs adapter Register.
// Held tools (Hold: true) are recorded for CI/catalog but are not registered on the MCP wire.
func PrepareAdapterRegistration(server *mcp.Server, schema SchemaWriter, register func(*mcp.Server, SchemaWriter)) {
	ResetToolRegistry()
	register(server, schema)
}
