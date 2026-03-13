package agentruntimemcp

import (
	"log"
	"os"
)

// Logger provides structured logging for the MCP SDK.
// Set MCP_LOG_LEVEL=debug for verbose logging.
var Logger = log.New(os.Stderr, "[agentruntime-mcp] ", log.LstdFlags)

func logDebug(format string, args ...any) {
	if os.Getenv("MCP_LOG_LEVEL") == "debug" {
		Logger.Printf("[DEBUG] "+format, args...)
	}
}

func logWarn(format string, args ...any) {
	Logger.Printf("[WARN] "+format, args...)
}

func logError(format string, args ...any) {
	Logger.Printf("[ERROR] "+format, args...)
}
