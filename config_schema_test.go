package agentruntimemcp

import "testing"

func TestConfigSchemaHasKeys(t *testing.T) {
	if configSchemaHasKeys(nil) {
		t.Fatal("nil should be false")
	}
	if configSchemaHasKeys(map[string]any{}) {
		t.Fatal("empty map should be false")
	}
	if !configSchemaHasKeys(map[string]any{"k": map[string]any{"type": "string"}}) {
		t.Fatal("non-empty should be true")
	}
}
