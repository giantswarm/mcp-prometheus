package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

	// Create a mock server context
	ctx := context.Background()
	sc, err := server.NewServerContext(ctx,
		server.WithPrometheusConfig(server.PrometheusConfig{
			URL: "http://localhost:9090",
		}),
		server.WithLogger(&TestLogger{}),
	)
	if err != nil {
		t.Fatalf("Failed to create server context: %v", err)
	}
	defer sc.Shutdown()

	err = RegisterPrometheusTools(s, sc)
	if err != nil {
		t.Fatalf("Failed to register tools: %v", err)
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
