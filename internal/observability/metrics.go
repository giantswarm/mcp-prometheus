package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds Prometheus counters and histograms for MCP tool invocations.
type Metrics struct {
	registry         *prometheus.Registry
	ToolCallsTotal   *prometheus.CounterVec
	ToolCallDuration *prometheus.HistogramVec
}

// NewMetrics creates a Prometheus registry and registers:
//   - Go runtime collector
//   - Process collector
//   - mcp_prometheus_tool_calls_total{tool,status}
//   - mcp_prometheus_tool_call_duration_seconds{tool}
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	factory := promauto.With(reg)
	return &Metrics{
		registry: reg,
		ToolCallsTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "mcp_prometheus",
				Name:      "tool_calls_total",
				Help:      "Total MCP tool calls, by tool name and status (success|error).",
			},
			[]string{"tool", "status"},
		),
		ToolCallDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "mcp_prometheus",
				Name:      "tool_call_duration_seconds",
				Help:      "Duration of MCP tool calls in seconds, by tool name.",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"tool"},
		),
	}
}

// Handler returns an HTTP handler that serves the Prometheus metrics page in
// OpenMetrics / text format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}
