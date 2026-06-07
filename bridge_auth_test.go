package agentruntimemcp

import (
	"testing"
)

func TestApplyAuthMappingBearer(t *testing.T) {
	h, err := ApplyAuthMapping(ConfigView{"access_token": "tok"}, map[string]any{
		"type": "bearer",
		"from": "access_token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := h.Get("Authorization"); got != "Bearer tok" {
		t.Fatalf("Authorization=%q", got)
	}
}

func TestApplyAuthMappingHeader(t *testing.T) {
	h, err := ApplyAuthMapping(ConfigView{"api_key": "k"}, map[string]any{
		"type": "header",
		"name": "X-API-Key",
		"from": "api_key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := h.Get("X-Api-Key"); got != "k" && got != h.Get("X-API-Key") {
		t.Fatalf("header=%q", got)
	}
}

func TestApplyAuthMappingNone(t *testing.T) {
	h, err := ApplyAuthMapping(nil, map[string]any{"type": "none"})
	if err != nil {
		t.Fatal(err)
	}
	if h.Get("Authorization") != "" {
		t.Fatal("expected no auth")
	}
}
