// Package agentruntimemcp provides an opinionated SDK for building MCP servers
// with the official modelcontextprotocol/go-sdk.
//
// Features:
//   - config.yaml loading (server, auth, config schema)
//   - Auth: token (Bearer/X-MCP-Token) or HMAC
//   - Control config resolution (POST /mcp/config)
//   - Schema endpoint (GET /mcp/config/schema)
//   - OpenTelemetry tracing (when tracing.enabled in config)
//   - Proxy: RunProxy to forward to another MCP server with overlay
//
// Use Run with a setup function to register tools. Config is available in
// tool handlers via ConfigFromContext(ctx).
//
// Version: 0.0.1
package agentruntimemcp
