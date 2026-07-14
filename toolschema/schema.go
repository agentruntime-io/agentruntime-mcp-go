// Package toolschema builds MCP tool input/output JSON Schemas from Go structs,

// extending github.com/google/jsonschema-go with optional field constraints.

//

// Use the agentschema struct tag for machine-readable constraints (minLength, maxLength,

// minimum, maximum, pattern, format).

// Use jsonschema for human descriptions only (same as jsonschema-go).

//

// Example:

//

//	type GetTaskInput struct {

//	    TaskID string `json:"task_id" jsonschema:"ClickUp task ID" agentschema:"minLength=1"`

//	    Limit  int    `json:"limit,omitempty" jsonschema:"Page size" agentschema:"minimum=1,maximum=100"`

//	}

package toolschema



import (

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

}



// ParseAgentschema parses agentschema tag values, e.g.

// "minLength=1,maxLength=64", "minimum=1,maximum=100", "pattern=^[a-z]+$".

func ParseAgentschema(tag string) (FieldConstraints, error) {

	var out FieldConstraints

	tag = strings.TrimSpace(tag)

	if tag == "" {

		return out, nil

	}

	for _, part := range strings.Split(tag, ",") {

		part = strings.TrimSpace(part)

		if part == "" {

			continue

		}

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

		default:

			return out, fmt.Errorf("agentschema: unsupported key %q (supported: minLength, maxLength, minimum, maximum, pattern, format)", key)

		}

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

		if constraints.MinLength != nil {

			prop.MinLength = constraints.MinLength

		}

		if constraints.MaxLength != nil {

			prop.MaxLength = constraints.MaxLength

		}

		if constraints.Minimum != nil {

			prop.Minimum = constraints.Minimum

		}

		if constraints.Maximum != nil {

			prop.Maximum = constraints.Maximum

		}

		if constraints.Pattern != "" {

			prop.Pattern = constraints.Pattern

		}

		if constraints.Format != "" {

			prop.Format = constraints.Format

		}

	}

	return nil

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

