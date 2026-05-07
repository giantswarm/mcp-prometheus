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
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/mcp-prometheus/internal/server"
)

const (
	apiQueryPath = "/api/v1/query"

	respKeyStatus       = "status"
	respKeyData         = "data"
	respKeyResult       = "result"
	respKeyResultType   = "resultType"
	respValSuccess      = "success"
	respValVector       = "vector"
	paramKeyQuery       = "query"
	bodyPrometheusReady = "Prometheus is Ready."
	readyMsg            = "ready"
	notReadyMsg         = "not ready"
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
	defer func() { _ = sc.Shutdown() }()

	if err := RegisterPrometheusTools(s, sc); err != nil {
		t.Fatalf("Failed to register tools: %v", err)
	}
}

func TestRegisterPrometheusToolsWithMiddleware(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == apiQueryPath {
			_ = json.NewEncoder(w).Encode(map[string]any{
				respKeyStatus: respValSuccess,
				respKeyData:   map[string]any{respKeyResultType: respValVector, respKeyResult: []any{}},
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
	defer func() { _ = sc.Shutdown() }()

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
	if called[0] != toolExecuteQuery {
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
			response: bodyPrometheusReady,
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
					_, _ = w.Write([]byte(tt.response))
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
		if r.URL.Path == apiQueryPath {
			response := map[string]any{
				respKeyStatus: respValSuccess,
				respKeyData: map[string]any{
					respKeyResultType: respValVector,
					respKeyResult:     []any{},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
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
	defer func() { _ = sc.Shutdown() }()

	client, err := NewClient(sc.PrometheusConfig(), sc.Logger())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Test valid request
	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: toolExecuteQuery,
			Arguments: map[string]any{
				paramKeyQuery: "up",
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
			Name:      toolExecuteQuery,
			Arguments: map[string]any{},
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
			response := map[string]any{
				respKeyStatus: respValSuccess,
				respKeyData: map[string]any{
					respKeyResultType: "matrix",
					respKeyResult:     []any{},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
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
	defer func() { _ = sc.Shutdown() }()

	client, err := NewClient(sc.PrometheusConfig(), sc.Logger())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Test valid request
	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: toolExecuteRangeQuery,
			Arguments: map[string]any{
				paramKeyQuery: "up",
				"start":       "2023-01-01T00:00:00Z",
				"end":         "2023-01-01T01:00:00Z",
				"step":        "1m",
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
			response := map[string]any{
				respKeyStatus: respValSuccess,
				respKeyData: map[string]any{
					"http_requests_total": []any{
						map[string]any{
							"type": "counter",
							"help": "Total HTTP requests",
							"unit": "",
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
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
	defer func() { _ = sc.Shutdown() }()

	client, err := NewClient(sc.PrometheusConfig(), sc.Logger())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Test valid request
	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "get_metric_metadata",
			Arguments: map[string]any{
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
		{name: readyMsg, statusCode: http.StatusOK, body: bodyPrometheusReady, wantReady: true},
		{name: notReadyMsg, statusCode: http.StatusServiceUnavailable, body: "Service Unavailable", wantReady: false},
		{name: "unexpected 500", statusCode: http.StatusInternalServerError, body: "internal error", wantReady: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
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
			_, _ = w.Write([]byte("not found"))
		case "/ready":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(readyMsg))
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
			name:        readyMsg,
			statusCode:  http.StatusOK,
			body:        bodyPrometheusReady,
			wantIsError: false,
			wantText:    readyMsg,
		},
		{
			name:        notReadyMsg,
			statusCode:  http.StatusServiceUnavailable,
			body:        "not yet",
			wantIsError: true,
			wantText:    notReadyMsg,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
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
			defer func() { _ = sc.Shutdown() }()

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
	defer func() { _ = sc.Shutdown() }()

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

func TestTruncateWithAdvice(t *testing.T) {
	const advice = "ADV"

	t.Run("under cap returns input verbatim", func(t *testing.T) {
		in := "small payload"
		if got := truncateWithAdvice(in, advice); got != in {
			t.Errorf("expected passthrough, got %q", got)
		}
	})

	t.Run("over cap with no nearby newline cuts at MaxResultLength", func(t *testing.T) {
		in := strings.Repeat("a", MaxResultLength+50)
		got := truncateWithAdvice(in, advice)
		if want := MaxResultLength + len(advice); len(got) != want {
			t.Errorf("expected total length %d, got %d", want, len(got))
		}
		if !strings.HasSuffix(got, advice) {
			t.Errorf("expected advice suffix, got tail %q", got[len(got)-len(advice):])
		}
	})

	t.Run("over cap with newline in trailing 1000 bytes cuts at newline", func(t *testing.T) {
		// Place a newline 50 bytes before MaxResultLength, then more content.
		head := strings.Repeat("a", MaxResultLength-50)
		in := head + "\n" + strings.Repeat("b", 200)
		got := truncateWithAdvice(in, advice)
		if want := (MaxResultLength - 50) + len(advice); len(got) != want {
			t.Errorf("expected total length %d (cut at newline), got %d", want, len(got))
		}
		if !strings.HasPrefix(got, head) {
			t.Errorf("expected payload to start with head; got prefix %q", got[:20])
		}
		if !strings.HasSuffix(got, advice) {
			t.Errorf("expected advice suffix, got tail %q", got[len(got)-len(advice):])
		}
	})
}

func TestHandleListLabelNamesTruncation(t *testing.T) {
	// Stub /api/v1/labels with enough names to push the formatted response
	// past MaxResultLength so the middleware cap fires.
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/labels" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		names := make([]string, 6000)
		for i := range names {
			names[i] = fmt.Sprintf("label_%05d", i)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			respKeyStatus: respValSuccess,
			respKeyData:   names,
		})
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
	defer func() { _ = sc.Shutdown() }()

	client, err := NewClient(sc.PrometheusConfig(), sc.Logger())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Exercise the production path: handler wrapped by truncationMiddleware.
	h := truncationMiddleware("list_label_names", discoveryAdvice, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleListLabelNames(ctx, req, client, sc)
	})

	result, err := h(ctx, mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected at least one content block")
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}

	// The formatted body itself must not exceed MaxResultLength; the appended
	// advice is allowed to push the total slightly past it.
	body := strings.TrimSuffix(tc.Text, discoveryAdvice)
	if len(body) > MaxResultLength {
		t.Errorf("formatted body exceeds cap: len=%d > MaxResultLength=%d", len(body), MaxResultLength)
	}
	// And the discovery advice must be appended.
	if !strings.HasSuffix(tc.Text, discoveryAdvice) {
		t.Errorf("expected discoveryAdvice suffix; tail=%q", tc.Text[max(0, len(tc.Text)-120):])
	}
}

func TestTruncationMiddleware(t *testing.T) {
	bigText := strings.Repeat("x", MaxResultLength+500)
	smallText := "ok"

	makeHandler := func(text string) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.TextContent{Type: contentTypeText, Text: text}},
			}, nil
		}
	}

	tests := []struct {
		name       string
		toolName   string
		advice     string
		text       string
		unlimited  bool
		wantSuffix string // empty: expect text returned verbatim
	}{
		{
			name:       "discovery tool truncates with discoveryAdvice",
			toolName:   "find_series",
			advice:     discoveryAdvice,
			text:       bigText,
			wantSuffix: discoveryAdvice,
		},
		{
			name:       "bulk tool truncates with bulkAdvice",
			toolName:   "get_rules",
			advice:     bulkAdvice,
			text:       bigText,
			wantSuffix: bulkAdvice,
		},
		{
			name:       "alerts tool truncates with alertsAdvice",
			toolName:   "get_alerts",
			advice:     alertsAdvice,
			text:       bigText,
			wantSuffix: alertsAdvice,
		},
		{
			name:       "query tool truncates with TruncationAdvice",
			toolName:   toolExecuteQuery,
			advice:     TruncationAdvice,
			text:       bigText,
			wantSuffix: TruncationAdvice,
		},
		{
			name:       "short text passes through untouched",
			toolName:   toolExecuteQuery,
			advice:     TruncationAdvice,
			text:       smallText,
			wantSuffix: "",
		},
		{
			name:       "unlimited=true bypasses on query tool",
			toolName:   toolExecuteQuery,
			advice:     TruncationAdvice,
			text:       bigText,
			unlimited:  true,
			wantSuffix: "",
		},
		{
			name:       "unlimited=true bypasses on range query tool",
			toolName:   toolExecuteRangeQuery,
			advice:     TruncationAdvice,
			text:       bigText,
			unlimited:  true,
			wantSuffix: "",
		},
		{
			name:       "unlimited=true is ignored on bulk tool",
			toolName:   "get_rules",
			advice:     bulkAdvice,
			text:       bigText,
			unlimited:  true,
			wantSuffix: bulkAdvice,
		},
		{
			name:       "unlimited=true is ignored on discovery tool",
			toolName:   "find_series",
			advice:     discoveryAdvice,
			text:       bigText,
			unlimited:  true,
			wantSuffix: discoveryAdvice,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := truncationMiddleware(tt.toolName, tt.advice, makeHandler(tt.text))
			req := mcp.CallToolRequest{Params: mcp.CallToolParams{Name: tt.toolName}}
			if tt.unlimited {
				req.Params.Arguments = map[string]any{"unlimited": "true"}
			}
			res, err := h(context.Background(), req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := res.Content[0].(mcp.TextContent).Text

			if tt.wantSuffix == "" {
				if got != tt.text {
					t.Errorf("expected passthrough; len got=%d want=%d", len(got), len(tt.text))
				}
				return
			}
			if !strings.HasSuffix(got, tt.wantSuffix) {
				t.Errorf("expected suffix %q; tail=%q", strings.TrimSpace(tt.wantSuffix)[:20], got[max(0, len(got)-120):])
			}
			if len(got) > MaxResultLength+len(tt.wantSuffix) {
				t.Errorf("truncated text longer than cap+advice: %d", len(got))
			}
		})
	}

	t.Run("truncates every oversized TextContent in a multi-block result", func(t *testing.T) {
		h := truncationMiddleware("find_series", discoveryAdvice, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: contentTypeText, Text: bigText},
					mcp.TextContent{Type: contentTypeText, Text: smallText},
					mcp.TextContent{Type: contentTypeText, Text: bigText},
				},
			}, nil
		})
		res, err := h(context.Background(), mcp.CallToolRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		first := res.Content[0].(mcp.TextContent).Text
		middle := res.Content[1].(mcp.TextContent).Text
		last := res.Content[2].(mcp.TextContent).Text
		if !strings.HasSuffix(first, discoveryAdvice) {
			t.Error("expected first block to be truncated")
		}
		if middle != smallText {
			t.Errorf("expected middle block untouched, got %q", middle)
		}
		if !strings.HasSuffix(last, discoveryAdvice) {
			t.Error("expected last block to be truncated")
		}
	})

	t.Run("propagates handler errors without modification", func(t *testing.T) {
		boom := fmt.Errorf("boom")
		h := truncationMiddleware(toolExecuteQuery, TruncationAdvice, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return nil, boom
		})
		_, err := h(context.Background(), mcp.CallToolRequest{})
		if err != boom {
			t.Errorf("expected boom propagated, got %v", err)
		}
	})

	t.Run("preserves IsError flag on tool error results", func(t *testing.T) {
		h := truncationMiddleware(toolExecuteQuery, TruncationAdvice, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{mcp.TextContent{Type: contentTypeText, Text: bigText}},
			}, nil
		})
		res, _ := h(context.Background(), mcp.CallToolRequest{})
		if !res.IsError {
			t.Error("expected IsError=true to be preserved")
		}
		got := res.Content[0].(mcp.TextContent).Text
		if !strings.HasSuffix(got, TruncationAdvice) {
			t.Error("expected truncation to still apply to error results")
		}
	})
}

// TestTruncateWithAdviceUTF8 verifies that truncation never lands mid-rune,
// so the resulting string remains valid UTF-8 even when the byte cap falls
// inside a multi-byte character.
func TestTruncateWithAdviceUTF8(t *testing.T) {
	const advice = "ADV"

	t.Run("backs off mid-rune cut to a valid boundary", func(t *testing.T) {
		// Build a payload where MaxResultLength sits inside a 4-byte rune
		// (U+1F4A1 LIGHT BULB = 0xF0 0x9F 0x92 0xA1). No newlines, so the
		// newline-anchor branch doesn't apply.
		const bulb = "\U0001F4A1"
		// Pad so a bulb crosses the MaxResultLength boundary.
		padLen := MaxResultLength - 2
		in := strings.Repeat("a", padLen) + bulb + strings.Repeat("b", 100)
		got := truncateWithAdvice(in, advice)
		body := strings.TrimSuffix(got, advice)
		if !utf8.ValidString(body) {
			t.Errorf("truncated body is not valid UTF-8: %q", body[len(body)-8:])
		}
		if !strings.HasSuffix(got, advice) {
			t.Error("expected advice suffix")
		}
	})
}
