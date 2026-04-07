package agentruntimemcp

// ConfigType constants for schema field types.
const (
	ConfigTypeString = "string"
	ConfigTypeNumber = "number"
	ConfigTypeBool   = "boolean"
)

// ConfigField describes a config key for the control server schema.
type ConfigField struct {
	Type        string
	DisplayName string
	Required    bool
	Default     any
}

// ConfigFieldDef is a unified schema definition with key. Use Field() + options to build.
type ConfigFieldDef struct {
	Key   string
	Field ConfigField
}

// ConfigFieldOpt is a functional option for ConfigField.
type ConfigFieldOpt func(*ConfigField)

// OptRequired marks the field as required.
func OptRequired() ConfigFieldOpt {
	return func(f *ConfigField) { f.Required = true }
}

// OptDefault sets the default value.
func OptDefault(v any) ConfigFieldOpt {
	return func(f *ConfigField) { f.Default = v }
}

// Field creates a ConfigFieldDef with key, type, displayName and options.
func Field(key, typ, displayName string, opts ...ConfigFieldOpt) ConfigFieldDef {
	f := ConfigField{Type: typ, DisplayName: displayName}
	for _, o := range opts {
		o(&f)
	}
	return ConfigFieldDef{Key: key, Field: f}
}

// StringField is shorthand for Field(key, ConfigTypeString, displayName, opts...).
func StringField(key, displayName string, opts ...ConfigFieldOpt) ConfigFieldDef {
	return Field(key, ConfigTypeString, displayName, opts...)
}

// SchemaWriter writes config schema fields. Adapters use this for type-safe registration.
type SchemaWriter interface {
	Add(key string, field ConfigField)
}

type schemaWriter struct {
	into map[string]any
}

func (s *schemaWriter) Add(key string, field ConfigField) {
	m := map[string]any{
		"type":        field.Type,
		"displayName": field.DisplayName,
		"required":    field.Required,
	}
	if field.Default != nil {
		m["default"] = field.Default
	}
	s.into[key] = m
}

// NewSchemaWriter creates a SchemaWriter that merges into the given map.
func NewSchemaWriter(into map[string]any) SchemaWriter {
	if into == nil {
		into = make(map[string]any)
	}
	return &schemaWriter{into: into}
}

// WriteSchema writes defs to sw. prefix is prepended to each key (e.g. "resend_" for router).
func WriteSchema(sw SchemaWriter, prefix string, defs []ConfigFieldDef) {
	if sw == nil {
		return
	}
	for _, d := range defs {
		key := prefix + d.Key
		sw.Add(key, d.Field)
	}
}

// configSchemaHasKeys is true when the adapter registered at least one config field.
// Empty registration means no Control-backed keys; Middleware skips POST /mcp/config for that adapter.
func configSchemaHasKeys(schema map[string]any) bool {
	return len(schema) > 0
}
