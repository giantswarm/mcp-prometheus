package observability

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const defaultServiceName = "mcp-prometheus"

// NewTracerProvider initialises an OTel TracerProvider.
//
// When OTEL_EXPORTER_OTLP_ENDPOINT is empty a no-op provider is returned and
// no network connections are made.  When the env var is set an OTLP HTTP
// exporter is created and the global TracerProvider is set.
//
// The second return value is a shutdown function that must be called on exit to
// flush any pending spans.
func NewTracerProvider(ctx context.Context) (trace.TracerProvider, func(context.Context) error, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return noop.NewTracerProvider(), func(context.Context) error { return nil }, nil
	}

	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = defaultServiceName
	}

	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithFromEnv(),
		resource.WithHost(),
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		// Non-fatal: fall back to the default resource.
		res = resource.Default()
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)

	return provider, provider.Shutdown, nil
}
