package agentruntimemcp

import (
	"fmt"
	"net/http"
	"strings"
)

// ApplyAuthMapping turns resolved Control config into outbound HTTP headers for the upstream MCP.
// auth_mapping shape:
//
//	{ "type": "none" }
//	{ "type": "bearer", "from": "access_token" }
//	{ "type": "header", "name": "X-API-Key", "from": "api_key" }
func ApplyAuthMapping(cfg ConfigView, mapping map[string]any) (http.Header, error) {
	h := make(http.Header)
	if mapping == nil {
		return h, nil
	}
	typ := strings.ToLower(strings.TrimSpace(stringFromMap(mapping, "type")))
	if typ == "" || typ == "none" {
		return h, nil
	}
	from := strings.TrimSpace(stringFromMap(mapping, "from"))
	if from == "" {
		from = strings.TrimSpace(stringFromMap(mapping, "value_from"))
	}
	val := configString(cfg, from)
	if val == "" && typ != "none" {
		return nil, fmt.Errorf("auth mapping requires config key %q", from)
	}
	switch typ {
	case "bearer":
		h.Set("Authorization", "Bearer "+val)
	case "header":
		name := strings.TrimSpace(stringFromMap(mapping, "name"))
		if name == "" {
			return nil, fmt.Errorf("auth mapping type header requires name")
		}
		h.Set(name, val)
	default:
		return nil, fmt.Errorf("unsupported auth_mapping type %q", typ)
	}
	return h, nil
}

func configString(cfg ConfigView, key string) string {
	if cfg == nil || key == "" {
		return ""
	}
	v, ok := cfg[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func stringFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
