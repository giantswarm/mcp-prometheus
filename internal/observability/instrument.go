package observability

import (
	"context"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Instrumentor wraps MCP tool handlers to record Prometheus metrics and
// OpenTelemetry spans for every tool invocation.
type Instrumentor struct {
	metrics *Metrics
	tracer  trace.Tracer
}

// NewInstrumentor creates an Instrumentor backed by the given metrics registry
// and tracer provider.
func NewInstrumentor(metrics *Metrics, tp trace.TracerProvider) *Instrumentor {
	return &Instrumentor{
		metrics: metrics,
		tracer:  tp.Tracer("mcp-prometheus"),
	}
}

// Wrap instruments a single MCP tool handler with Prometheus metrics and an
// OTel span.  It satisfies the ToolMiddleware signature expected by
// RegisterPrometheusTools.
func (i *Instrumentor) Wrap(
	toolName string,
	next func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error),
) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		ctx, span := i.tracer.Start(ctx, "mcp.tool/"+toolName,
			trace.WithAttributes(attribute.String("mcp.tool.name", toolName)),
			trace.WithSpanKind(trace.SpanKindServer),
		)
		defer span.End()

		start := time.Now()
		result, err := next(ctx, req)
		duration := time.Since(start).Seconds()

		status := "success"
		switch {
		case err != nil:
			status = "error"
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		case result != nil && result.IsError:
			status = "error"
			span.SetStatus(codes.Error, "tool returned an error result")
		}

		i.metrics.ToolCallsTotal.WithLabelValues(toolName, status).Inc()
		i.metrics.ToolCallDuration.WithLabelValues(toolName).Observe(duration)

		return result, err
	}
}
