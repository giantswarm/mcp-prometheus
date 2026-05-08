package observability

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"go.opentelemetry.io/otel/trace/noop"
)

// ── Metrics ───────────────────────────────────────────────────────────────────

func TestNewMetricsRegistersCounterAndHistogram(t *testing.T) {
	m := NewMetrics()
	if m.ToolCallsTotal == nil {
		t.Fatal("ToolCallsTotal should not be nil")
	}
	if m.ToolCallDuration == nil {
		t.Fatal("ToolCallDuration should not be nil")
	}
}

func TestMetricsHandlerServes200(t *testing.T) {
	m := NewMetrics()
	rr := httptest.NewRecorder()
	m.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 from /metrics, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "go_goroutines") {
		t.Error("expected Go runtime metrics in /metrics output")
	}
}

func TestMetricsHandlerContainsToolCallsMetric(t *testing.T) {
	m := NewMetrics()
	m.ToolCallsTotal.WithLabelValues("test_tool", "success").Inc()
	m.ToolCallsTotal.WithLabelValues("test_tool", "success").Inc()
	m.ToolCallsTotal.WithLabelValues("test_tool", "error").Inc()

	rr := httptest.NewRecorder()
	m.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(rr.Body.String(), "mcp_prometheus_tool_calls_total") {
		t.Error("expected mcp_prometheus_tool_calls_total in output")
	}
}

// ── Instrumentor ──────────────────────────────────────────────────────────────

func noopInstrumentor() *Instrumentor {
	return NewInstrumentor(NewMetrics(), noop.NewTracerProvider())
}

func TestInstrumentorWrapSuccess(t *testing.T) {
	inst := noopInstrumentor()

	called := false
	handler := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		called = true
		return &mcp.CallToolResult{}, nil
	}

	result, err := inst.Wrap("my_tool", handler)(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("underlying handler was not called")
	}
	if result.IsError {
		t.Error("result should not be marked as error")
	}

	body := metricsBody(t, inst.metrics)
	if !strings.Contains(body, `tool="my_tool"`) {
		t.Error("expected tool label in metrics output")
	}
	if !strings.Contains(body, `status="success"`) {
		t.Error("expected status=success label in metrics output")
	}
}

func TestInstrumentorWrapHandlerError(t *testing.T) {
	inst := noopInstrumentor()

	handler := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return nil, errors.New("boom")
	}

	_, err := inst.Wrap("failing_tool", handler)(context.Background(), mcp.CallToolRequest{})
	if err == nil {
		t.Fatal("expected error to be propagated")
	}
	if !strings.Contains(metricsBody(t, inst.metrics), `status="error"`) {
		t.Error("expected status=error when handler returns error")
	}
}

func TestInstrumentorWrapToolResultIsError(t *testing.T) {
	inst := noopInstrumentor()

	handler := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{IsError: true}, nil
	}

	result, err := inst.Wrap("error_result_tool", handler)(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("IsError flag should be preserved in result")
	}
	if !strings.Contains(metricsBody(t, inst.metrics), `status="error"`) {
		t.Error("expected status=error when result.IsError=true")
	}
}

func TestInstrumentorWrapNilResult(t *testing.T) {
	inst := noopInstrumentor()

	// A handler that incorrectly returns (nil, nil) must be counted as an error.
	handler := func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return nil, nil
	}

	result, err := inst.Wrap("nil_result_tool", handler)(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result != nil {
		t.Error("nil result should be passed through unchanged")
	}
	if !strings.Contains(metricsBody(t, inst.metrics), `status="error"`) {
		t.Error("expected status=error when handler returns (nil, nil)")
	}
}

func TestInstrumentorWrapRecordsLatency(t *testing.T) {
	inst := noopInstrumentor()

	_, _ = inst.Wrap("latency_tool", func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})(context.Background(), mcp.CallToolRequest{})

	if !strings.Contains(metricsBody(t, inst.metrics), "mcp_prometheus_tool_call_duration_seconds") {
		t.Error("expected duration histogram in metrics output")
	}
}

// metricsBody renders the Prometheus metrics page and returns the body string.
func metricsBody(t *testing.T, m *Metrics) string {
	t.Helper()
	rr := httptest.NewRecorder()
	m.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return rr.Body.String()
}
