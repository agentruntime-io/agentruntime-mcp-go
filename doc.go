// Package agentruntimemcp provides an opinionated SDK for building MCP servers
// with the official modelcontextprotocol/go-sdk.
//
// Features:
//   - config.yaml loading (server, tracing, config schema)
//   - Control config resolution (POST /mcp/config) when the adapter registers at least one config field; empty registration skips Control for that adapter (per-route in the router)
//   - Optional schema config from environment: case-insensitive env var per schema key or AR_<key>; env wins over Control per key; env-only satisfies required keys when Control is missing or fails (see middleware)
//   - Set MCP_LOG_LEVEL=debug for runtime_context / header diagnostics; missing instance_id/server_id logs a WARN before calling Control
//   - Schema endpoint (GET /mcp/config/schema)
//   - OpenTelemetry tracing (when tracing.enabled in config)
//   - Proxy: RunProxy to forward to another MCP server with overlay
//
// Use Run with a setup function, or RunWithRegistry for plugin-style adapters.
// Adapters register via RegisterAdapter in init(); use blank imports to load.
// Config is available in tool handlers via ConfigFromContext(ctx).
//
// Future: optional custom ingress validation (e.g. extra headers) is not implemented yet.
//
// Version: 0.0.1
package agentruntimemcp
