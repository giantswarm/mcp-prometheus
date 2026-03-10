package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/mcp-prometheus/internal/server"
)

// discardLogger returns a *slog.Logger that discards all output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRegisterPrometheusTools(t *testing.T) {
	s := mcpserver.NewMCPServer("test", "1.0.0")

	ctx := context.Background()
	sc, err := server.NewServerContext(ctx,
		server.WithPrometheusConfig(server.PrometheusConfig{URL: "http://localhost:9090"}),
		server.WithSlogLogger(discardLogger()),
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
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
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
		server.WithSlogLogger(discardLogger()),
	)
	if err != nil {
		t.Fatalf("Failed to create server context: %v", err)
	}
	defer sc.Shutdown()

	// Middleware that records the tool name for every invocation through the
	// server's dispatch path.
	var called []string
	mw := ToolMiddleware(func(name string, next func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			called = append(called, name)
			return next(ctx, req)
		}
	})

	s := mcpserver.NewMCPServer("test", "1.0.0", mcpserver.WithToolCapabilities(true))
	if err := RegisterPrometheusTools(s, sc, mw); err != nil {
		t.Fatalf("RegisterPrometheusTools: %v", err)
	}

	// Dispatch through HandleMessage so we exercise the actual registered path.
	msg := []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tools/call",
		"params": {
			"name": "execute_query",
			"arguments": {"query": "up", "prometheus_url": "` + mockServer.URL + `"}
		}
	}`)
	s.HandleMessage(ctx, msg)

	if len(called) == 0 {
		t.Fatal("expected middleware to be invoked via server dispatch, but called is empty")
	}
	if called[0] != "execute_query" {
		t.Errorf("expected first middleware call for execute_query, got %q", called[0])
	}
}

func TestClient(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		response string
		testFunc func(context.Context, *Client) error
	}{
		{
			name:     "ExecuteQuery",
			endpoint: "/api/v1/query",
			response: `{"status": "success", "data": {"resultType": "vector", "result": []}}`,
			testFunc: func(ctx context.Context, c *Client) error {
				_, err := c.ExecuteQuery(ctx, "up", "")
				return err
			},
		},
		{
			name:     "ExecuteRangeQuery",
			endpoint: "/api/v1/query_range",
			response: `{"status": "success", "data": {"resultType": "matrix", "result": []}}`,
			testFunc: func(ctx context.Context, c *Client) error {
				_, err := c.ExecuteRangeQuery(ctx, "up", "2023-01-01T00:00:00Z", "2023-01-01T01:00:00Z", "1m")
				return err
			},
		},
		{
			name:     "GetMetricMetadata",
			endpoint: "/api/v1/metadata",
			response: `{"status": "success", "data": {"http_requests_total": [{"type": "counter", "help": "Total HTTP requests", "unit": ""}]}}`,
			testFunc: func(ctx context.Context, c *Client) error {
				result, err := c.GetMetricMetadata(ctx, "http_requests_total")
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
			testFunc: func(ctx context.Context, c *Client) error {
				_, err := c.GetTargets(ctx)
				return err
			},
		},
		{
			name:     "CheckReady",
			endpoint: "/-/ready",
			response: "Prometheus is Ready.",
			testFunc: func(ctx context.Context, c *Client) error {
				status, err := c.CheckReady(ctx)
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
			client, err := NewClient(config, discardLogger())
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}

			// Run test
			if err := tt.testFunc(context.Background(), client); err != nil {
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
		server.WithSlogLogger(discardLogger()),
	)
	if err != nil {
		t.Fatalf("Failed to create server context: %v", err)
	}
	defer sc.Shutdown()

	client, err := NewClient(sc.PrometheusConfig(), sc.Logger())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

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
		server.WithSlogLogger(discardLogger()),
	)
	if err != nil {
		t.Fatalf("Failed to create server context: %v", err)
	}
	defer sc.Shutdown()

	client, err := NewClient(sc.PrometheusConfig(), sc.Logger())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

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
		server.WithSlogLogger(discardLogger()),
	)
	if err != nil {
		t.Fatalf("Failed to create server context: %v", err)
	}
	defer sc.Shutdown()

	client, err := NewClient(sc.PrometheusConfig(), sc.Logger())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

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

			client, err := NewClient(server.PrometheusConfig{URL: srv.URL}, discardLogger())
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
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
	client, err := NewClient(server.PrometheusConfig{URL: "http://127.0.0.1:1"}, discardLogger())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.CheckReady(context.Background())
	if err == nil {
		t.Fatal("expected connection error, got nil")
	}
}

// TestCheckReadyMimirFallback verifies that when /-/ready returns 404
// (Mimir nginx gateway has no such route), CheckReady falls back to /ready.
func TestCheckReadyMimirFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/-/ready":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("not found")) //nolint:errcheck
		case "/ready":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ready")) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client, err := NewClient(server.PrometheusConfig{URL: srv.URL + "/prometheus"}, discardLogger())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	status, err := client.CheckReady(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Ready {
		t.Errorf("expected Ready=true after fallback, got false (status %d: %s)", status.StatusCode, status.Message)
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
			wantIsError: true,
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
				server.WithSlogLogger(discardLogger()),
			)
			if err != nil {
				t.Fatalf("failed to create server context: %v", err)
			}
			defer sc.Shutdown()

			client, err := NewClient(sc.PrometheusConfig(), sc.Logger())
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
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
		server.WithSlogLogger(discardLogger()),
	)
	if err != nil {
		t.Fatalf("failed to create server context: %v", err)
	}
	defer sc.Shutdown()

	client, err := NewClient(sc.PrometheusConfig(), sc.Logger())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	result, err := handleCheckReady(ctx, mcp.CallToolRequest{}, client, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for connection failure")
	}
}
