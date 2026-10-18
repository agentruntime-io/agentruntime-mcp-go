// Package toolschema builds MCP tool input/output JSON Schemas from Go structs,
// extending github.com/google/jsonschema-go with optional field constraints.
//
// Use the agentschema struct tag for machine-readable constraints (minLength, maxLength,
// minimum, maximum, pattern, format, enum, default).
// Use jsonschema for human descriptions only (same as jsonschema-go).
package toolschema

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// FieldConstraints holds optional JSON Schema constraints from an agentschema tag.
type FieldConstraints struct {
	MinLength *int
	MaxLength *int
	Minimum   *float64
	Maximum   *float64
	Pattern   string
	Format    string
	Enum      []any
	Default   json.RawMessage
	HasDefault bool
}

// ParseAgentschema parses agentschema tag values, e.g.
// "minLength=1,maxLength=64", "enum=opt_in,opt_out", "default=30".
func ParseAgentschema(tag string) (FieldConstraints, error) {
	var out FieldConstraints
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return out, nil
	}
	for _, part := range mergeAgentschemaSegments(tag) {
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			return out, fmt.Errorf("agentschema: invalid segment %q (expected key=value)", part)
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if val == "" {
			return out, fmt.Errorf("agentschema: %s requires a value", key)
		}
		switch key {
		case "minLength", "maxLength":
			n, err := strconv.Atoi(val)
			if err != nil {
				return out, fmt.Errorf("agentschema: %s must be an integer, got %q", key, val)
			}
			if key == "minLength" {
				out.MinLength = &n
			} else {
				out.MaxLength = &n
			}
		case "minimum", "maximum":
			f, err := strconv.ParseFloat(val, 64)
			if err != nil {
				return out, fmt.Errorf("agentschema: %s must be a number, got %q", key, val)
			}
			if key == "minimum" {
				out.Minimum = &f
			} else {
				out.Maximum = &f
			}
		case "pattern":
			if _, err := regexp.Compile(val); err != nil {
				return out, fmt.Errorf("agentschema: pattern: %w", err)
			}
			out.Pattern = val
		case "format":
			out.Format = val
		case "enum":
			values, err := parseEnumLiteral(val)
			if err != nil {
				return out, fmt.Errorf("agentschema: enum: %w", err)
			}
			out.Enum = values
		case "default":
			raw, err := json.Marshal(val)
			if err != nil {
				return out, fmt.Errorf("agentschema: default: %w", err)
			}
			out.Default = json.RawMessage(raw)
			out.HasDefault = true
		default:
			return out, fmt.Errorf("agentschema: unsupported key %q (supported: minLength, maxLength, minimum, maximum, pattern, format, enum, default)", key)
		}
	}
	return out, nil
}

// mergeAgentschemaSegments splits agentschema tags on commas, but keeps comma-separated
// enum values attached to the enum= segment (e.g. enum=opt_in,opt_out).
func mergeAgentschemaSegments(tag string) []string {
	raw := strings.Split(tag, ",")
	merged := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "=") {
			merged = append(merged, part)
			continue
		}
		if len(merged) == 0 {
			return []string{part}
		}
		last := merged[len(merged)-1]
		if strings.HasPrefix(last, "enum=") {
			merged[len(merged)-1] = last + "," + part
			continue
		}
		return []string{part}
	}
	return merged
}

func parseEnumLiteral(val string) ([]any, error) {
	parts := strings.Split(val, ",")
	out := make([]any, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("requires at least one value")
	}
	return out, nil
}

// For builds a JSON Schema for T using jsonschema.For, then applies agentschema tags.
func For[T any](opts *jsonschema.ForOptions) (*jsonschema.Schema, error) {
	schema, err := jsonschema.For[T](opts)
	if err != nil {
		return nil, err
	}
	if err := applyAgentschemaTags(schema, reflect.TypeFor[T]()); err != nil {
		return nil, err
	}
	return schema, nil
}

func applyAgentschemaTags(schema *jsonschema.Schema, t reflect.Type) error {
	if schema == nil {
		return nil
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		tag, ok := field.Tag.Lookup("agentschema")
		if !ok {
			continue
		}
		constraints, err := ParseAgentschema(tag)
		if err != nil {
			return fmt.Errorf("%s.%s: %w", t.Name(), field.Name, err)
		}
		jsonName, ok := jsonFieldName(field)
		if !ok {
			continue
		}
		prop := schema.Properties[jsonName]
		if prop == nil {
			return fmt.Errorf("%s.%s: agentschema on unknown json field %q", t.Name(), field.Name, jsonName)
		}
		if err := applyConstraintsToProp(prop, constraints, field.Type); err != nil {
			return fmt.Errorf("%s.%s: %w", t.Name(), field.Name, err)
		}
	}
	return nil
}

func applyConstraintsToProp(prop *jsonschema.Schema, c FieldConstraints, fieldType reflect.Type) error {
	if c.MinLength != nil {
		prop.MinLength = c.MinLength
	}
	if c.MaxLength != nil {
		prop.MaxLength = c.MaxLength
	}
	if c.Minimum != nil {
		prop.Minimum = c.Minimum
	}
	if c.Maximum != nil {
		prop.Maximum = c.Maximum
	}
	if c.Pattern != "" {
		prop.Pattern = c.Pattern
	}
	if c.Format != "" {
		prop.Format = c.Format
	}
	if len(c.Enum) > 0 {
		typed, err := coerceEnumValues(c.Enum, fieldType)
		if err != nil {
			return err
		}
		prop.Enum = typed
	}
	if c.HasDefault {
		typed, err := coerceDefaultValue(c.Default, fieldType)
		if err != nil {
			return err
		}
		prop.Default = typed
	}
	return nil
}

func coerceEnumValues(values []any, fieldType reflect.Type) ([]any, error) {
	fieldType = dereferenceType(fieldType)
	out := make([]any, 0, len(values))
	for _, v := range values {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("enum values must be strings in agentschema tag")
		}
		typed, err := coerceScalarLiteral(s, fieldType)
		if err != nil {
			return nil, err
		}
		out = append(out, typed)
	}
	return out, nil
}

func coerceDefaultValue(raw json.RawMessage, fieldType reflect.Type) (json.RawMessage, error) {
	fieldType = dereferenceType(fieldType)
	var lit string
	if err := json.Unmarshal(raw, &lit); err != nil {
		return nil, err
	}
	typed, err := coerceScalarLiteral(lit, fieldType)
	if err != nil {
		return nil, err
	}
	return json.Marshal(typed)
}

func coerceScalarLiteral(lit string, fieldType reflect.Type) (any, error) {
	switch fieldType.Kind() {
	case reflect.String:
		return lit, nil
	case reflect.Bool:
		v, err := strconv.ParseBool(lit)
		if err != nil {
			return nil, fmt.Errorf("invalid bool default/enum value %q", lit)
		}
		return v, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(lit, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid integer default/enum value %q", lit)
		}
		return int(n), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(lit, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid unsigned default/enum value %q", lit)
		}
		return uint(n), nil
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(lit, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid float default/enum value %q", lit)
		}
		return f, nil
	default:
		return lit, nil
	}
}

func dereferenceType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func jsonFieldName(field reflect.StructField) (string, bool) {
	tag, ok := field.Tag.Lookup("json")
	if !ok {
		return "", false
	}
	if tag == "-" {
		return "", false
	}
	name, _, _ := strings.Cut(tag, ",")
	name = strings.TrimSpace(name)
	if name == "" {
		return field.Name, true
	}
	return name, true
}
