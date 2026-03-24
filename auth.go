package agentruntimemcp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func extractToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	if t := r.Header.Get("X-MCP-Token"); t != "" {
		return strings.TrimSpace(t)
	}
	return ""
}

func isSchemaEndpoint(r *http.Request) bool {
	return isSchemaEndpointForMount(r, "/mcp")
}

// isSchemaEndpointForMount returns true if the request is for the config schema.
// mountPath is the MCP base path (e.g. "/mcp" or "/github/mcp"); schema is at mountPath+"/config/schema".
func isSchemaEndpointForMount(r *http.Request, mountPath string) bool {
	if r.Method != http.MethodGet {
		return false
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	schemaPath := strings.TrimSuffix(mountPath, "/") + "/config/schema"
	return path == schemaPath
}

// validateAuth validates token or HMAC based on env. Returns nil if auth passes.
func validateAuth(r *http.Request) error {
	mode := os.Getenv("MCP_AUTH_MODE")
	if mode == "" {
		mode = "token"
	}

	if mode == "token" {
		expected := os.Getenv("MCP_AUTH_TOKEN")
		if expected == "" {
			return nil
		}
		token := extractToken(r)
		if token == "" || token != expected {
			return ErrAuthFailed
		}
		return nil
	}

	if mode == "hmac" {
		keyID := os.Getenv("MCP_HMAC_KEY_ID")
		secret := os.Getenv("MCP_HMAC_SECRET")
		if keyID == "" || secret == "" {
			return nil
		}
		reqKeyID := r.Header.Get("X-MCP-KeyId")
		if reqKeyID == "" {
			reqKeyID = r.Header.Get("X-MCP-Key")
		}
		ts := r.Header.Get("X-MCP-Timestamp")
		sig := r.Header.Get("X-MCP-Signature")
		if reqKeyID == "" || ts == "" || sig == "" {
			return ErrAuthFailed
		}
		tsInt, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			return ErrAuthFailed
		}
		if abs(time.Now().Unix()-tsInt) > 300 {
			return ErrAuthFailed
		}
		if reqKeyID != keyID {
			return ErrAuthFailed
		}
		method := r.Method
		if method == "" {
			method = "POST"
		}
		path := r.URL.Path
		if path == "" {
			path = "/mcp"
		}
		base := strconv.FormatInt(tsInt, 10) + "\n" + method + "\n" + path
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(base))
		expected := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(expected), []byte(sig)) {
			return ErrAuthFailed
		}
		return nil
	}

	return nil
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
