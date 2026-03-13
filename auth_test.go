package agentruntimemcp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestExtractToken(t *testing.T) {
	tests := []struct {
		name   string
		auth   string
		xToken string
		want   string
	}{
		{"bearer", "Bearer abc123", "", "abc123"},
		{"x-mcp-token", "", "xyz789", "xyz789"},
		{"bearer wins", "Bearer first", "second", "first"},
		{"empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			if tt.xToken != "" {
				req.Header.Set("X-MCP-Token", tt.xToken)
			}
			got := extractToken(req)
			if got != tt.want {
				t.Errorf("extractToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateAuthToken(t *testing.T) {
	os.Setenv("MCP_AUTH_MODE", "token")
	os.Setenv("MCP_AUTH_TOKEN", "secret123")
	defer func() {
		os.Unsetenv("MCP_AUTH_MODE")
		os.Unsetenv("MCP_AUTH_TOKEN")
	}()

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer secret123")
	if err := validateAuth(req); err != nil {
		t.Errorf("valid token should pass: %v", err)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req2.Header.Set("Authorization", "Bearer wrong")
	if err := validateAuth(req2); err == nil {
		t.Error("invalid token should fail")
	}
}
