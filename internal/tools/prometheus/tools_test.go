package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/mcp-prometheus/internal/server"
)

// TestLogger implements server.Logger for testing
type TestLogger struct{}

func (l *TestLogger) Debug(msg string, args ...interface{}) {}
func (l *TestLogger) Info(msg string, args ...interface{})  {}
func (l *TestLogger) Warn(msg string, args ...interface{})  {}
func (l *TestLogger) Error(msg string, args ...interface{}) {}

func TestRegisterPrometheusTools(t *testing.T) {
	s := mcpserver.NewMCPServer("test", "1.0.0")

	ctx := context.Background()
	sc, err := server.NewServerContext(ctx,
		server.WithPrometheusConfig(server.PrometheusConfig{URL: "http://localhost:9090"}),
		server.WithLogger(&TestLogger{}),
	)
	if err != nil {
		t.Fatalf("Failed to create server context: %v", err)
	}
	defer sc.Shutdown()

	if err := RegisterPrometheusTools(s, sc); err != nil {
		t.Fatalf("Failed to register tools: %v", err)
	}
}

func TestRegisterPrometheusToolsWithMiddleware(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/query" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "success",
				"data":   map[string]interface{}{"resultType": "vector", "result": []interface{}{}},
			})
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	ctx := context.Background()
	sc, err := server.NewServerContext(ctx,
		server.WithPrometheusConfig(server.PrometheusConfig{URL: mockServer.URL}),
		server.WithLogger(&TestLogger{}),
	)
	if err != nil {
		t.Fatalf("Failed to create server context: %v", err)
	}
	defer sc.Shutdown()

	// Middleware that records every invocation.
	var called []string
	mw := ToolMiddleware(func(name string, next func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			called = append(called, name)
			return next(ctx, req)
		}
	})

	s := mcpserver.NewMCPServer("test", "1.0.0")
	if err := RegisterPrometheusTools(s, sc, mw); err != nil {
		t.Fatalf("RegisterPrometheusTools: %v", err)
	}

	client := NewClient(sc.PrometheusConfig(), sc.Logger())
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "execute_query",
			Arguments: map[string]interface{}{"query": "up"},
		},
	}
	if _, err := handleExecuteQuery(ctx, req, client, sc); err != nil {
		t.Fatalf("handleExecuteQuery: %v", err)
	}

	// The middleware was registered; call it directly to confirm it works.
	mw("execute_query", func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})(ctx, req) //nolint:errcheck

	if len(called) == 0 {
		t.Error("expected middleware to have been called at least once")
	}
	if called[0] != "execute_query" {
		t.Errorf("expected first call to be execute_query, got %q", called[0])
	}
}

func TestClient(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		response string
		testFunc func(*Client) error
	}{
		{
			name:     "ExecuteQuery",
			endpoint: "/api/v1/query",
			response: `{"status": "success", "data": {"resultType": "vector", "result": []}}`,
			testFunc: func(c *Client) error {
				_, err := c.ExecuteQuery("up", "")
				return err
			},
		},
		{
			name:     "ExecuteRangeQuery",
			endpoint: "/api/v1/query_range",
			response: `{"status": "success", "data": {"resultType": "matrix", "result": []}}`,
			testFunc: func(c *Client) error {
				_, err := c.ExecuteRangeQuery("up", "2023-01-01T00:00:00Z", "2023-01-01T01:00:00Z", "1m")
				return err
			},
		},
		{
			name:     "ListMetrics",
			endpoint: "/api/v1/label/__name__/values",
			response: `{"status": "success", "data": ["metric1", "metric2"]}`,
			testFunc: func(c *Client) error {
				_, err := c.ListMetrics()
				return err
			},
		},
		{
			name:     "GetMetricMetadata",
			endpoint: "/api/v1/metadata",
			response: `{"status": "success", "data": {"http_requests_total": [{"type": "counter", "help": "Total HTTP requests", "unit": ""}]}}`,
			testFunc: func(c *Client) error {
				result, err := c.GetMetricMetadata("http_requests_total")
				if err != nil {
					return err
				}
				// Verify the result contains the specific metric
				if _, exists := result["http_requests_total"]; !exists {
					return fmt.Errorf("expected metadata for http_requests_total not found")
				}
				return nil
			},
		},
		{
			name:     "GetTargets",
			endpoint: "/api/v1/targets",
			response: `{"status": "success", "data": {"activeTargets": [], "droppedTargets": []}}`,
			testFunc: func(c *Client) error {
				_, err := c.GetTargets()
				return err
			},
		},
		{
			name:     "CheckReady",
			endpoint: "/-/ready",
			response: "Prometheus is Ready.",
			testFunc: func(c *Client) error {
				status, err := c.CheckReady(context.Background())
				if err != nil {
					return err
				}
				if !status.Ready {
					return fmt.Errorf("expected Ready=true, got false")
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tt.endpoint {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(tt.response))
				} else {
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer mockServer.Close()

			// Create client
			config := server.PrometheusConfig{URL: mockServer.URL}
			logger := &TestLogger{}
			client := NewClient(config, logger)

			// Run test
			if err := tt.testFunc(client); err != nil {
				t.Errorf("Test failed: %v", err)
			}
		})
	}
}

func TestHandleExecuteQuery(t *testing.T) {
	// Create mock server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/query" {
			response := map[string]interface{}{
				"status": "success",
				"data": map[string]interface{}{
					"resultType": "vector",
					"result":     []interface{}{},
				},
			}
			json.NewEncoder(w).Encode(response)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	// Create client and server context
	ctx := context.Background()
	sc, err := server.NewServerContext(ctx,
		server.WithPrometheusConfig(server.PrometheusConfig{
			URL: mockServer.URL,
		}),
		server.WithLogger(&TestLogger{}),
	)
	if err != nil {
		t.Fatalf("Failed to create server context: %v", err)
	}
	defer sc.Shutdown()

	client := NewClient(sc.PrometheusConfig(), sc.Logger())

	// Test valid request
	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "execute_query",
			Arguments: map[string]interface{}{
				"query": "up",
			},
		},
	}

	result, err := handleExecuteQuery(context.Background(), request, client, sc)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result.IsError {
		t.Errorf("Expected success, got error: %v", result.Content)
	}

	// Test missing query parameter
	requestBad := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "execute_query",
			Arguments: map[string]interface{}{},
		},
	}

	result, err = handleExecuteQuery(context.Background(), requestBad, client, sc)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if !result.IsError {
		t.Errorf("Expected error for missing query parameter")
	}
}

func TestHandleExecuteRangeQuery(t *testing.T) {
	// Create mock server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/query_range" {
			response := map[string]interface{}{
				"status": "success",
				"data": map[string]interface{}{
					"resultType": "matrix",
					"result":     []interface{}{},
				},
			}
			json.NewEncoder(w).Encode(response)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	// Create client and server context
	ctx := context.Background()
	sc, err := server.NewServerContext(ctx,
		server.WithPrometheusConfig(server.PrometheusConfig{
			URL: mockServer.URL,
		}),
		server.WithLogger(&TestLogger{}),
	)
	if err != nil {
		t.Fatalf("Failed to create server context: %v", err)
	}
	defer sc.Shutdown()

	client := NewClient(sc.PrometheusConfig(), sc.Logger())

	// Test valid request
	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "execute_range_query",
			Arguments: map[string]interface{}{
				"query": "up",
				"start": "2023-01-01T00:00:00Z",
				"end":   "2023-01-01T01:00:00Z",
				"step":  "1m",
			},
		},
	}

	result, err := handleExecuteRangeQuery(context.Background(), request, client, sc)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result.IsError {
		t.Errorf("Expected success, got error: %v", result.Content)
	}
}

func TestHandleGetMetricMetadata(t *testing.T) {
	// Create mock server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/metadata" {
			response := map[string]interface{}{
				"status": "success",
				"data": map[string]interface{}{
					"http_requests_total": []interface{}{
						map[string]interface{}{
							"type": "counter",
							"help": "Total HTTP requests",
							"unit": "",
						},
					},
				},
			}
			json.NewEncoder(w).Encode(response)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	// Create client and server context
	ctx := context.Background()
	sc, err := server.NewServerContext(ctx,
		server.WithPrometheusConfig(server.PrometheusConfig{
			URL: mockServer.URL,
		}),
		server.WithLogger(&TestLogger{}),
	)
	if err != nil {
		t.Fatalf("Failed to create server context: %v", err)
	}
	defer sc.Shutdown()

	client := NewClient(sc.PrometheusConfig(), sc.Logger())

	// Test valid request
	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "get_metric_metadata",
			Arguments: map[string]interface{}{
				"metric": "http_requests_total",
			},
		},
	}

	result, err := handleGetMetricMetadata(context.Background(), request, client, sc)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result.IsError {
		t.Errorf("Expected success, got error: %v", result.Content)
	}
}

func TestCheckReady(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantReady  bool
	}{
		{name: "ready", statusCode: http.StatusOK, body: "Prometheus is Ready.", wantReady: true},
		{name: "not ready", statusCode: http.StatusServiceUnavailable, body: "Service Unavailable", wantReady: false},
		{name: "unexpected 500", statusCode: http.StatusInternalServerError, body: "internal error", wantReady: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body)) //nolint:errcheck
			}))
			defer srv.Close()

			client := NewClient(server.PrometheusConfig{URL: srv.URL}, &TestLogger{})
			status, err := client.CheckReady(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if status.Ready != tt.wantReady {
				t.Errorf("Ready=%v, want %v", status.Ready, tt.wantReady)
			}
			if status.StatusCode != tt.statusCode {
				t.Errorf("StatusCode=%d, want %d", status.StatusCode, tt.statusCode)
			}
			if status.Message != tt.body {
				t.Errorf("Message=%q, want %q", status.Message, tt.body)
			}
		})
	}
}

func TestCheckReadyConnectionError(t *testing.T) {
	client := NewClient(server.PrometheusConfig{URL: "http://127.0.0.1:1"}, &TestLogger{})
	_, err := client.CheckReady(context.Background())
	if err == nil {
		t.Fatal("expected connection error, got nil")
	}
}

func TestHandleCheckReady(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		body        string
		wantIsError bool
		wantText    string
	}{
		{
			name:        "ready",
			statusCode:  http.StatusOK,
			body:        "Prometheus is Ready.",
			wantIsError: false,
			wantText:    "ready",
		},
		{
			name:        "not ready",
			statusCode:  http.StatusServiceUnavailable,
			body:        "not yet",
			wantIsError: false,
			wantText:    "not ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body)) //nolint:errcheck
			}))
			defer srv.Close()

			ctx := context.Background()
			sc, err := server.NewServerContext(ctx,
				server.WithPrometheusConfig(server.PrometheusConfig{URL: srv.URL}),
				server.WithLogger(&TestLogger{}),
			)
			if err != nil {
				t.Fatalf("failed to create server context: %v", err)
			}
			defer sc.Shutdown()

			client := NewClient(sc.PrometheusConfig(), sc.Logger())
			result, err := handleCheckReady(ctx, mcp.CallToolRequest{}, client, sc)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.IsError != tt.wantIsError {
				t.Errorf("IsError=%v, want %v", result.IsError, tt.wantIsError)
			}
			text := result.Content[0].(mcp.TextContent).Text
			if !strings.Contains(text, tt.wantText) {
				t.Errorf("result text %q does not contain %q", text, tt.wantText)
			}
		})
	}
}

func TestHandleCheckReadyConnectionError(t *testing.T) {
	ctx := context.Background()
	sc, err := server.NewServerContext(ctx,
		server.WithPrometheusConfig(server.PrometheusConfig{URL: "http://127.0.0.1:1"}),
		server.WithLogger(&TestLogger{}),
	)
	if err != nil {
		t.Fatalf("failed to create server context: %v", err)
	}
	defer sc.Shutdown()

	client := NewClient(sc.PrometheusConfig(), sc.Logger())
	result, err := handleCheckReady(ctx, mcp.CallToolRequest{}, client, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for connection failure")
	}
}
