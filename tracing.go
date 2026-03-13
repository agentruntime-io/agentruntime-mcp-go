package agentruntimemcp

import (
	"context"
	"net/http"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// shouldEnableTracing returns true if tracing should be enabled.
// Disabled when: OTEL_SDK_DISABLED=true or config tracing.enabled=false
func shouldEnableTracing(cfg *ServerConfig) bool {
	if os.Getenv("OTEL_SDK_DISABLED") != "" {
		switch os.Getenv("OTEL_SDK_DISABLED") {
		case "true", "1", "yes":
			return false
		}
	}
	if cfg != nil && cfg.Tracing != nil && !cfg.Tracing.Enabled {
		return false
	}
	return true
}

var tracerProvider *sdktrace.TracerProvider

// initTracing initializes OpenTelemetry with OTLP HTTP exporter.
// Call before starting the server when tracing is enabled.
func initTracing(cfg *ServerConfig) error {
	if !shouldEnableTracing(cfg) {
		return nil
	}
	otelEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if otelEndpoint == "" {
		otelEndpoint = "127.0.0.1:4318"
	}
	// Strip scheme for otlptracehttp (e.g. "http://localhost:4318" -> "localhost:4318")
	otelEndpoint = strings.TrimPrefix(otelEndpoint, "https://")
	otelEndpoint = strings.TrimPrefix(otelEndpoint, "http://")
	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = "agentruntime-mcp"
	}

	exporter, err := otlptracehttp.New(
		context.Background(),
		otlptracehttp.WithEndpoint(otelEndpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		logError("failed to create OTLP trace exporter: %v", err)
		return err
	}

	tracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
		)),
	)
	otel.SetTracerProvider(tracerProvider)
	logDebug("OpenTelemetry tracing initialized")
	return nil
}

// wrapWithTracing wraps an HTTP handler with OpenTelemetry instrumentation.
func wrapWithTracing(cfg *ServerConfig, handler http.Handler) http.Handler {
	if !shouldEnableTracing(cfg) {
		return handler
	}
	if tracerProvider == nil {
		if err := initTracing(cfg); err != nil {
			return handler
		}
	}
	return otelhttp.NewHandler(handler, "mcp")
}
