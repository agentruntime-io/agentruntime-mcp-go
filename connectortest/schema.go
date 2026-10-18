package connectortest

import (
	"fmt"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

// AssertStringPropertyMinLength fails when prop is missing or minLength differs.
func AssertStringPropertyMinLength(t *testing.T, schema *jsonschema.Schema, prop string, want int) {
	t.Helper()
	if schema == nil {
		t.Fatal("schema is nil")
	}
	p := schema.Properties[prop]
	if p == nil {
		t.Fatalf("property %q not found in schema", prop)
	}
	if p.MinLength == nil {
		t.Fatalf("property %q: expected minLength %d, got nil", prop, want)
	}
	if *p.MinLength != want {
		t.Fatalf("property %q: expected minLength %d, got %d", prop, want, *p.MinLength)
	}
}

// AssertRequiredStringIDsHaveMinLength checks every required string property whose
// name ends with _id has minLength=1. Skips optional properties (not in schema.Required).
func AssertRequiredStringIDsHaveMinLength(schema *jsonschema.Schema) error {
	if schema == nil {
		return fmt.Errorf("schema is nil")
	}
	required := make(map[string]bool, len(schema.Required))
	for _, name := range schema.Required {
		required[name] = true
	}
	for name, prop := range schema.Properties {
		if !required[name] {
			continue
		}
		if prop == nil || prop.Type != "string" {
			continue
		}
		if len(name) >= 3 && name[len(name)-3:] == "_id" {
			if prop.MinLength == nil || *prop.MinLength < 1 {
				return fmt.Errorf("required string %q must have agentschema minLength=1", name)
			}
		}
	}
	return nil
}
