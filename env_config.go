package agentruntimemcp

import (
	"fmt"
	"os"
	"strings"
)

// Schema env lookup (case-insensitive variable names):
//   - <schemaKey> e.g. api_key, API_KEY, Api_Key
//   - AR_<schemaKey> e.g. AR_api_key (power-user / disambiguation)
//
// Values are trimmed; empty values are ignored. Per-key: env overrides Control after merge.

func newLowercaseEnvIndex() map[string]string {
	idx := make(map[string]string)
	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		name, val := kv[:eq], kv[eq+1:]
		if strings.TrimSpace(val) == "" {
			continue
		}
		idx[strings.ToLower(name)] = val
	}
	return idx
}

func lookupSchemaEnv(idx map[string]string, schemaKey string) (string, bool) {
	if v, ok := idx[strings.ToLower(schemaKey)]; ok {
		return strings.TrimSpace(v), true
	}
	if v, ok := idx[strings.ToLower("AR_"+schemaKey)]; ok {
		return strings.TrimSpace(v), true
	}
	return "", false
}

func envOverridesFromSchema(schema map[string]any, idx map[string]string) map[string]string {
	out := make(map[string]string)
	for k := range schema {
		if v, ok := lookupSchemaEnv(idx, k); ok && v != "" {
			out[k] = v
		}
	}
	return out
}

func valueAsString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

func requiredConfigSatisfied(schema map[string]any, cfg ConfigView) bool {
	if len(schema) == 0 {
		return true
	}
	for key, def := range schema {
		meta, ok := def.(map[string]any)
		if !ok {
			continue
		}
		req, _ := meta["required"].(bool)
		if !req {
			continue
		}
		if valueAsString(cfg[key]) != "" {
			continue
		}
		if _, has := meta["default"]; has {
			continue
		}
		return false
	}
	return true
}

func envAloneSatisfiesRequired(schema map[string]any, idx map[string]string) bool {
	envOnly := ConfigView{}
	for k := range schema {
		if v, ok := lookupSchemaEnv(idx, k); ok && v != "" {
			envOnly[k] = v
		}
	}
	return requiredConfigSatisfied(schema, envOnly)
}

// mergeControlWithEnvPriority copies Control config, then overwrites every schema key that appears in envKeys.
func mergeControlWithEnvPriority(control ConfigView, envKeys map[string]string, schema map[string]any) ConfigView {
	final := ConfigView{}
	for k, v := range control {
		final[k] = v
	}
	for k := range schema {
		if v, ok := envKeys[k]; ok {
			final[k] = v
		}
	}
	return final
}
