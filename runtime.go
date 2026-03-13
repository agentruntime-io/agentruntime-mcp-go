package agentruntimemcp

import (
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Run creates and runs the MCP server. The setup function receives the server
// and config schema; use it to register tools via mcp.AddTool. Config is
// available in tool handlers via ConfigFromContext(ctx).
func Run(configPath string, setup func(server *mcp.Server, configSchema map[string]any)) error {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return err
	}
	return RunWithConfig(cfg, setup)
}

// RunWithConfig runs the MCP server with the given config.
func RunWithConfig(cfg *ServerConfig, setup func(server *mcp.Server, configSchema map[string]any)) error {
	if cfg == nil {
		cfg = &ServerConfig{}
	}
	server, configSchema := MakeServer(cfg)
	setup(server, configSchema)

	host := os.Getenv("HOST")
	if host == "" && cfg.Server != nil {
		host = cfg.Server.Host
	}
	if host == "" {
		host = "127.0.0.1"
	}
	port := 8000
	if p := os.Getenv("PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	} else if cfg.Server != nil && cfg.Server.Port > 0 {
		port = cfg.Server.Port
	}
	addr := host + ":" + strconv.Itoa(port)

	mcpHandler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return server
	}, nil)
	handler := Middleware(configSchema, mcpHandler)
	handler = wrapWithTracing(cfg, handler)

	log.Printf("MCP server listening on %s", addr)
	return http.ListenAndServe(addr, handler)
}

// MakeServer creates an MCP server and returns the config schema for middleware.
func MakeServer(cfg *ServerConfig) (*mcp.Server, map[string]any) {
	if cfg == nil {
		cfg = &ServerConfig{}
	}
	name := "MCPServer"
	if cfg.Server != nil && cfg.Server.Name != "" {
		name = cfg.Server.Name
	}
	configSchema := cfg.Config
	if configSchema == nil {
		configSchema = make(map[string]any)
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    name,
		Version: "0.0.1",
	}, nil)

	return server, configSchema
}
