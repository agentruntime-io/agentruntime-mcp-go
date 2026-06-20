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

// ApplyHeaderMappings sets outbound headers from resolved Control config.
// Each mapping entry: { "name": "X-Header", "from": "config_key" }.
// Empty config values are skipped. Missing required keys return an error.
func ApplyHeaderMappings(cfg ConfigView, mappings []map[string]any) (http.Header, error) {
	h := make(http.Header)
	for _, m := range mappings {
		if m == nil {
			continue
		}
		name := strings.TrimSpace(stringFromMap(m, "name"))
		from := strings.TrimSpace(stringFromMap(m, "from"))
		if from == "" {
			from = strings.TrimSpace(stringFromMap(m, "value_from"))
		}
		if name == "" {
			return nil, fmt.Errorf("header mapping requires name")
		}
		if from == "" {
			return nil, fmt.Errorf("header mapping for %q requires from", name)
		}
		val := configString(cfg, from)
		if val == "" {
			return nil, fmt.Errorf("header mapping requires config key %q", from)
		}
		h.Set(name, val)
	}
	return h, nil
}

// ApplyBridgeHeaders merges auth_mapping and header_mappings from bridge metadata into outbound headers.
// auth_mapping is applied first; header_mappings follow (incoming request headers win on duplicate names).
func ApplyBridgeHeaders(cfg ConfigView, bridge map[string]any) (http.Header, error) {
	h := make(http.Header)
	if bridge == nil {
		return h, nil
	}
	var authMap map[string]any
	if am, ok := bridge["auth_mapping"].(map[string]any); ok {
		authMap = am
	}
	authHdr, err := ApplyAuthMapping(cfg, authMap)
	if err != nil {
		return nil, err
	}
	for k, vals := range authHdr {
		for _, v := range vals {
			h.Set(k, v)
		}
	}
	var headerMappings []map[string]any
	switch raw := bridge["header_mappings"].(type) {
	case []any:
		for _, item := range raw {
			if m, ok := item.(map[string]any); ok {
				headerMappings = append(headerMappings, m)
			}
		}
	case []map[string]any:
		headerMappings = raw
	}
	if len(headerMappings) == 0 {
		return h, nil
	}
	hdrMappings, err := ApplyHeaderMappings(cfg, headerMappings)
	if err != nil {
		return nil, err
	}
	for k, vals := range hdrMappings {
		for _, v := range vals {
			h.Set(k, v)
		}
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
