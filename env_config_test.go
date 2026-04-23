package agentruntimemcp

import (
	"testing"
)

func TestLookupSchemaEnv_CaseInsensitive(t *testing.T) {
	idx := map[string]string{
		"api_key":     "v1",
		"ar_my_token": "v2",
	}
	if v, ok := lookupSchemaEnv(idx, "API_KEY"); !ok || v != "v1" {
		t.Fatalf("API_KEY: got %q %v", v, ok)
	}
	if v, ok := lookupSchemaEnv(idx, "my_token"); !ok || v != "v2" {
		t.Fatalf("AR_ my_token: got %q %v", v, ok)
	}
}

func TestEnvAloneSatisfiesRequired(t *testing.T) {
	schema := map[string]any{
		"api_key":    map[string]any{"type": "string", "required": true},
		"from_email": map[string]any{"type": "string", "required": true},
		"optional_x": map[string]any{"type": "string", "required": false},
	}
	idx := map[string]string{
		"api_key":    "k",
		"from_email": "a@b.c",
	}
	if !envAloneSatisfiesRequired(schema, idx) {
		t.Fatal("expected satisfied")
	}
	idx2 := map[string]string{"api_key": "k"}
	if envAloneSatisfiesRequired(schema, idx2) {
		t.Fatal("expected not satisfied")
	}
}

func TestMergeControlWithEnvPriority(t *testing.T) {
	schema := map[string]any{
		"a": map[string]any{},
		"b": map[string]any{},
	}
	control := ConfigView{"a": "from-control", "b": "from-control-b", "extra": "x"}
	envKeys := map[string]string{"a": "from-env"}
	final := mergeControlWithEnvPriority(control, envKeys, schema)
	if final["a"] != "from-env" {
		t.Fatalf("a: %v", final["a"])
	}
	if final["b"] != "from-control-b" {
		t.Fatalf("b: %v", final["b"])
	}
	if final["extra"] != "x" {
		t.Fatalf("extra: %v", final["extra"])
	}
}

func TestRequiredConfigSatisfied_WithDefault(t *testing.T) {
	schema := map[string]any{
		"api_base": map[string]any{"required": false, "default": "https://api.example"},
		"api_key":  map[string]any{"required": true},
	}
	cfg := ConfigView{"api_key": "k"}
	if !requiredConfigSatisfied(schema, cfg) {
		t.Fatal("expected satisfied")
	}
}
