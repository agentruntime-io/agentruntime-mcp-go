package agentruntimemcp

import (
	"os"
	"strings"
)

func envBoolTrue(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
