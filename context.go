package agentruntimemcp

import "context"

type configKey struct{}

// ConfigView is the resolved config from the control server.
type ConfigView map[string]any

// WithConfig returns a context with the given config attached.
func WithConfig(ctx context.Context, cfg ConfigView) context.Context {
	return context.WithValue(ctx, configKey{}, cfg)
}

// ConfigFromContext returns the config from context, or nil if not set.
func ConfigFromContext(ctx context.Context) ConfigView {
	v := ctx.Value(configKey{})
	if v == nil {
		return nil
	}
	if c, ok := v.(ConfigView); ok {
		return c
	}
	return nil
}

// ConfigGetStr reads a string from cfg. Tries prefix+key first, then key (for standalone vs router).
func ConfigGetStr(cfg ConfigView, prefix, key, defaultValue string) string {
	if cfg == nil {
		return defaultValue
	}
	keys := []string{key}
	if prefix != "" {
		keys = []string{prefix + key, key}
	}
	for _, k := range keys {
		v, ok := cfg[k]
		if !ok || v == nil {
			continue
		}
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return defaultValue
}
