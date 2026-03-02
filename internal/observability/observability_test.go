package observability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"go.opentelemetry.io/otel/trace/noop"
)

// ── Health ────────────────────────────────────────────────────────────────────

func TestHealthzAlwaysOK(t *testing.T) {
	h := &Health{}
	for _, ready := range []bool{false, true} {
		h.SetReady(ready)
		rr := httptest.NewRecorder()
		h.HealthzHandler()(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if rr.Code != http.StatusOK {
			t.Errorf("ready=%v: expected 200, got %d", ready, rr.Code)
		}
		if body := rr.Body.String(); body != "ok" {
			t.Errorf("ready=%v: expected body 'ok', got %q", ready, body)
		}
	}
}

func TestReadyzNotReadyByDefault(t *testing.T) {
	h := &Health{}
	rr := httptest.NewRecorder()
	h.ReadyzHandler()(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 before SetReady, got %d", rr.Code)
	}
}

func TestReadyzReadyAfterSetReady(t *testing.T) {
	h := &Health{}
	h.SetReady(true)
	rr := httptest.NewRecorder()
	h.ReadyzHandler()(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 after SetReady(true), got %d", rr.Code)
	}
	if body := rr.Body.String(); body != "ready" {
		t.Errorf("expected body 'ready', got %q", body)
	}
}

func TestReadyzTransitions(t *testing.T) {
	h := &Health{}
	h.SetReady(true)
	h.SetReady(false)
	rr := httptest.NewRecorder()
	h.ReadyzHandler()(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 after SetReady(false), got %d", rr.Code)
	}
}

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

	inst.Wrap("latency_tool", func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})(context.Background(), mcp.CallToolRequest{}) //nolint:errcheck

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

// ── OTel ──────────────────────────────────────────────────────────────────────

func TestNewTracerProviderNoOpWhenEnvUnset(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	tp, shutdown, err := NewTracerProvider(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer shutdown(context.Background()) //nolint:errcheck

	if tp == nil {
		t.Fatal("expected non-nil TracerProvider")
	}
	_, span := tp.Tracer("test").Start(context.Background(), "test-span")
	if span.IsRecording() {
		t.Error("no-op provider should produce non-recording spans")
	}
	span.End()
}

// ── Server (NewServer + RunServer) ────────────────────────────────────────────

func TestNewServerRoutes(t *testing.T) {
	m := NewMetrics()
	h := &Health{}
	h.SetReady(true)
	mux := NewServer(m, h)

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/metrics", http.StatusOK},
		{"/healthz", http.StatusOK},
		{"/readyz", http.StatusOK},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rr.Code != tc.want {
				t.Errorf("GET %s: expected %d, got %d", tc.path, tc.want, rr.Code)
			}
		})
	}
}

func TestRunServerShutdownOnContextCancel(t *testing.T) {
	m := NewMetrics()
	h := &Health{}
	mux := NewServer(m, h)

	addr, err := freeTCPAddr()
	if err != nil {
		t.Fatalf("could not find free port: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunServer(ctx, addr, mux) }()

	// Wait until the server is listening.
	if err := waitForHTTP(addr, 2*time.Second); err != nil {
		cancel()
		t.Fatalf("server did not start: %v", err)
	}

	// Quick sanity-check that it serves /healthz.
	resp, err := http.Get(fmt.Sprintf("http://%s/healthz", addr))
	if err != nil {
		cancel()
		t.Fatalf("GET /healthz failed: %v", err)
	}
	io.Copy(io.Discard, resp.Body) //nolint:errcheck
	resp.Body.Close()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("RunServer returned error on graceful shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("RunServer did not shut down within 5s")
	}
}

// freeTCPAddr asks the OS for an available TCP port on loopback and returns
// the address string "127.0.0.1:<port>".
func freeTCPAddr() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr, nil
}

// waitForHTTP polls addr until an HTTP GET to / succeeds or the deadline passes.
func waitForHTTP(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://%s/healthz", addr))
		if err == nil {
			io.Copy(io.Discard, resp.Body) //nolint:errcheck
			resp.Body.Close()
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("server at %s not ready after %s", addr, timeout)
}
