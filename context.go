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
