package prometheus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/mcp-prometheus/internal/server"
)

// TestInputSchemaValidation_RejectsUnknownProperty pins the motivating
// scenario from giantswarm/giantswarm#36458: a caller that sends a typo'd or
// stale property to a tool must receive a structured tool execution error
// that names the offending property, so the model can self-correct.
//
// This is an end-to-end check that:
//  1. WithInputSchemaValidation is wired into the server in cmd/serve.go, and
//  2. WithSchemaAdditionalProperties(false) is set in registerPrometheusTools
//     so unknown properties are rejected for every registered tool.
func TestInputSchemaValidation_RejectsUnknownProperty(t *testing.T) {
	srv, _, cleanup := newValidatingServer(t)
	defer cleanup()

	resp := dispatchToolCall(t, srv, toolExecuteQuery, map[string]any{
		"quary": "up", // intentional typo of "query"
	})

	jr, ok := resp.(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("expected JSON-RPC response, got %T", resp)
	}
	result, ok := jr.Result.(*mcp.CallToolResult)
	if !ok {
		t.Fatalf("expected *mcp.CallToolResult, got %T", jr.Result)
	}
	if !result.IsError {
		t.Fatal("expected validation to mark result as error")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected error content, got none")
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if !strings.Contains(tc.Text, "quary") {
		t.Fatalf("validation error should mention the offending property; got: %s", tc.Text)
	}
}

// TestInputSchemaValidation_AllToolsRejectAdditional locks in the invariant
// that every registered tool's input schema has additionalProperties: false.
// Without this, a future tool author who registers a tool outside the shared
// registerPrometheusTools helper would silently lose strict validation.
func TestInputSchemaValidation_AllToolsRejectAdditional(t *testing.T) {
	srv, _, cleanup := newValidatingServer(t)
	defer cleanup()

	tools := srv.ListTools()
	if len(tools) == 0 {
		t.Fatal("expected registered tools, got none")
	}
	for name, st := range tools {
		ap := st.Tool.InputSchema.AdditionalProperties
		b, ok := ap.(bool)
		if !ok || b {
			t.Errorf("tool %q: additionalProperties must be false, got %#v", name, ap)
		}
	}
}

// TestInputSchemaValidation_AcceptsKnownProperty makes sure a well-formed call
// passes validation and reaches the handler, so the rejection test above is
// meaningful (i.e. it isn't a side effect of an unrelated server-level reject).
func TestInputSchemaValidation_AcceptsKnownProperty(t *testing.T) {
	srv, mockURL, cleanup := newValidatingServer(t)
	defer cleanup()

	resp := dispatchToolCall(t, srv, toolExecuteQuery, map[string]any{
		paramKeyQuery:    "up",
		"prometheus_url": mockURL,
	})

	jr, ok := resp.(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("expected JSON-RPC response, got %T", resp)
	}
	result, ok := jr.Result.(*mcp.CallToolResult)
	if !ok {
		t.Fatalf("expected *mcp.CallToolResult, got %T", jr.Result)
	}
	if result.IsError {
		text := ""
		if len(result.Content) > 0 {
			if tc, ok := result.Content[0].(mcp.TextContent); ok {
				text = tc.Text
			}
		}
		t.Fatalf("well-formed call should not be rejected; got error: %s", text)
	}
}

// newValidatingServer spins up an MCP server with input schema validation
// enabled (matching cmd/serve.go) and registers all Prometheus tools against
// a mock Prometheus endpoint. Returns the server, the mock URL, and a cleanup
// function the caller must defer.
func newValidatingServer(t *testing.T) (*mcpserver.MCPServer, string, func()) {
	t.Helper()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == apiQueryPath {
			_ = json.NewEncoder(w).Encode(map[string]any{
				respKeyStatus: respValSuccess,
				respKeyData:   map[string]any{respKeyResultType: respValVector, respKeyResult: []any{}},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))

	sc, err := server.NewServerContext(context.Background(),
		server.WithPrometheusConfig(server.PrometheusConfig{URL: mockServer.URL}),
		server.WithSlogLogger(discardLogger()),
	)
	if err != nil {
		mockServer.Close()
		t.Fatalf("Failed to create server context: %v", err)
	}

	srv := mcpserver.NewMCPServer("test", "0.0.0",
		mcpserver.WithToolCapabilities(true),
		mcpserver.WithInputSchemaValidation(),
	)
	if err := RegisterPrometheusTools(srv, sc); err != nil {
		_ = sc.Shutdown()
		mockServer.Close()
		t.Fatalf("RegisterPrometheusTools: %v", err)
	}

	cleanup := func() {
		_ = sc.Shutdown()
		mockServer.Close()
	}
	return srv, mockServer.URL, cleanup
}

func dispatchToolCall(t *testing.T, srv *mcpserver.MCPServer, toolName string, args map[string]any) mcp.JSONRPCMessage {
	t.Helper()
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return srv.HandleMessage(context.Background(), raw)
}
