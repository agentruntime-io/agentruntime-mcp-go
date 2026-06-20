package agentruntimemcp

import (
	"testing"
)

func TestApplyAuthMappingBearer(t *testing.T) {
	cfg := ConfigView{"access_token": "tok123"}
	h, err := ApplyAuthMapping(cfg, map[string]any{
		"type": "bearer",
		"from": "access_token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := h.Get("Authorization"); got != "Bearer tok123" {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestApplyAuthMappingNone(t *testing.T) {
	h, err := ApplyAuthMapping(ConfigView{}, map[string]any{"type": "none"})
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 0 {
		t.Fatalf("expected no headers, got %v", h)
	}
}

func TestApplyHeaderMappingsGrafana(t *testing.T) {
	cfg := ConfigView{
		"grafana_url":              "https://acme.grafana.net",
		"service_account_token":    "glsa_xxx",
	}
	mappings := []map[string]any{
		{"name": "X-Grafana-URL", "from": "grafana_url"},
		{"name": "X-Grafana-Service-Account-Token", "from": "service_account_token"},
	}
	h, err := ApplyHeaderMappings(cfg, mappings)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.Get("X-Grafana-URL"); got != "https://acme.grafana.net" {
		t.Fatalf("X-Grafana-URL = %q", got)
	}
	if got := h.Get("X-Grafana-Service-Account-Token"); got != "glsa_xxx" {
		t.Fatalf("token header = %q", got)
	}
}

func TestApplyHeaderMappingsMissingKey(t *testing.T) {
	_, err := ApplyHeaderMappings(ConfigView{}, []map[string]any{
		{"name": "X-Grafana-URL", "from": "grafana_url"},
	})
	if err == nil {
		t.Fatal("expected error for missing config key")
	}
}

func TestApplyBridgeHeadersCombined(t *testing.T) {
	cfg := ConfigView{
		"access_token":          "google-tok",
		"grafana_url":             "https://acme.grafana.net",
		"service_account_token":   "glsa_xxx",
	}
	bridge := map[string]any{
		"auth_mapping": map[string]any{"type": "none"},
		"header_mappings": []any{
			map[string]any{"name": "X-Grafana-URL", "from": "grafana_url"},
			map[string]any{"name": "X-Grafana-Service-Account-Token", "from": "service_account_token"},
		},
	}
	h, err := ApplyBridgeHeaders(cfg, bridge)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.Get("X-Grafana-URL"); got != "https://acme.grafana.net" {
		t.Fatalf("X-Grafana-URL = %q", got)
	}
}

func TestApplyBridgeHeadersLegacyAuthOnly(t *testing.T) {
	cfg := ConfigView{"access_token": "tok"}
	bridge := map[string]any{
		"auth_mapping": map[string]any{"type": "bearer", "from": "access_token"},
	}
	h, err := ApplyBridgeHeaders(cfg, bridge)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.Get("Authorization"); got != "Bearer tok" {
		t.Fatalf("Authorization = %q", got)
	}
}
