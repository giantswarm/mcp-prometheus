// Package observability provides the Prometheus metrics + OTel-instrumented
// tool middleware specific to mcp-prometheus.
//
// Cross-cutting plumbing — slog factory, OTel tracer init, /healthz +
// /readyz handlers, graceful HTTP shutdown — is imported from
// github.com/giantswarm/mcp-toolkit and wired in cmd/serve.go. This package
// owns only the bits that are shaped by mcp-prometheus's specific needs:
//
//   - [NewMetrics] — Prometheus registry with per-tool counters and duration
//     histograms (mcp_prometheus_tool_calls_total, mcp_prometheus_tool_call_duration_seconds).
//   - [NewInstrumentor] — wraps each tool handler to record spans + metrics.
package observability
