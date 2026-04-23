package agentruntimemcp

import (
	"net/http"
	"net/http/httptest"
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
