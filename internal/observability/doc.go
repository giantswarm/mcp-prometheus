// Package observability provides Prometheus metrics, OpenTelemetry tracing,
// health/readiness probes, and the HTTP server that exposes them for the
// mcp-prometheus process.
//
// The main entry points are:
//
//   - [NewMetrics] — creates a custom Prometheus registry with per-tool counters
//     and duration histograms.
//   - [NewTracerProvider] — returns a no-op TracerProvider by default; switches to
//     OTLP HTTP export when OTEL_EXPORTER_OTLP_ENDPOINT is set.
//   - [NewInstrumentor] — wraps MCP tool handlers to record spans and metrics.
//   - [Health] — tracks readiness state; exposes /healthz and /readyz handlers.
//   - [NewServer] / [RunServer] — build and run the observability HTTP mux.
package observability
