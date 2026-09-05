package prometheus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
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
		paramKeyQuery:      "up",
		paramPrometheusURL: mockURL,
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
	return newValidatingServerWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == apiQueryPath {
			_ = json.NewEncoder(w).Encode(map[string]any{
				respKeyStatus: respValSuccess,
				respKeyData:   map[string]any{respKeyResultType: respValVector, respKeyResult: []any{}},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
}

// newValidatingServerWithHandler is newValidatingServer with a caller-supplied
// mock Prometheus handler, for tests that need to observe the upstream request.
func newValidatingServerWithHandler(t *testing.T, handler http.HandlerFunc) (*mcpserver.MCPServer, string, func()) {
	t.Helper()

	mockServer := httptest.NewServer(handler)

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

// callToolResult unwraps a tools/call JSON-RPC message into its result.
func callToolResult(t *testing.T, resp mcp.JSONRPCMessage) *mcp.CallToolResult {
	t.Helper()
	jr, ok := resp.(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("expected JSON-RPC response, got %T", resp)
	}
	result, ok := jr.Result.(*mcp.CallToolResult)
	if !ok {
		t.Fatalf("expected *mcp.CallToolResult, got %T", jr.Result)
	}
	return result
}

func resultText(result *mcp.CallToolResult) string {
	if len(result.Content) == 0 {
		return ""
	}
	if tc, ok := result.Content[0].(mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

// TestInputSchemaValidation_GetRulesFilters is the regression test for the
// gazelle symptom: calling get_rules with {"type":"alert"} was rejected by the
// strict input schema with `<root>: &{Properties:[type]}` because the tool
// declared no filter parameters at all. The filters must now be part of the
// schema, pass validation, and reach Prometheus as query parameters — while
// wrong types and unknown names are still rejected before the handler runs.
func TestInputSchemaValidation_GetRulesFilters(t *testing.T) {
	var gotQuery url.Values
	srv, mockURL, cleanup := newValidatingServerWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != rulesEndpoint {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotQuery = r.URL.Query()
		emptyRulesResponse(w, r)
	})
	defer cleanup()

	t.Run("schema declares the filters", func(t *testing.T) {
		tool, ok := srv.ListTools()["get_rules"]
		if !ok {
			t.Fatal("get_rules is not registered")
		}
		props := tool.Tool.InputSchema.Properties
		for _, name := range []string{paramRuleType, paramRuleName, paramRuleGroup, paramRuleFile, paramExcludeAlerts} {
			if _, ok := props[name]; !ok {
				t.Errorf("get_rules input schema lacks property %q", name)
			}
		}
		typeProp, _ := props[paramRuleType].(map[string]any)
		enum, _ := typeProp["enum"].([]string)
		if !reflect.DeepEqual(enum, []string{RuleTypeAlert, RuleTypeRecord}) {
			t.Errorf("type enum = %#v, want [%s %s]", typeProp["enum"], RuleTypeAlert, RuleTypeRecord)
		}
	})

	t.Run("filters pass validation and reach Prometheus", func(t *testing.T) {
		gotQuery = nil
		result := callToolResult(t, dispatchToolCall(t, srv, "get_rules", map[string]any{
			paramPrometheusURL: mockURL,
			paramRuleType:      RuleTypeAlert,
			paramRuleName:      []any{testRuleName},
			paramRuleGroup:     []any{testRuleGroup},
			paramRuleFile:      []any{testRuleFile},
			paramExcludeAlerts: true,
		}))
		if result.IsError {
			t.Fatalf("well-formed get_rules call must not be rejected; got: %s", resultText(result))
		}
		want := url.Values{
			rulesQueryType:          {RuleTypeAlert},
			rulesQueryRuleName:      {testRuleName},
			rulesQueryRuleGroup:     {testRuleGroup},
			rulesQueryFile:          {testRuleFile},
			rulesQueryExcludeAlerts: {queryTrue},
		}
		if !reflect.DeepEqual(gotQuery, want) {
			t.Errorf("upstream query\n got: %v\nwant: %v", gotQuery, want)
		}
	})

	t.Run("no filters still sends a bare request", func(t *testing.T) {
		gotQuery = nil
		result := callToolResult(t, dispatchToolCall(t, srv, "get_rules", map[string]any{
			paramPrometheusURL: mockURL,
		}))
		if result.IsError {
			t.Fatalf("bare get_rules call must not be rejected; got: %s", resultText(result))
		}
		if len(gotQuery) != 0 {
			t.Errorf("expected no query parameters, got %v", gotQuery)
		}
	})

	rejected := []struct {
		name string
		args map[string]any
		want string // substring the validation error must contain
	}{
		{"type outside the enum", map[string]any{paramRuleType: testInvalidRuleType}, testInvalidRuleType},
		{"rule_name as a string", map[string]any{paramRuleName: testRuleName}, paramRuleName},
		{"exclude_alerts as a string", map[string]any{paramExcludeAlerts: queryTrue}, paramExcludeAlerts},
		{"unknown property", map[string]any{"rulename": []any{"x"}}, "rulename"},
	}
	for _, tc := range rejected {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			gotQuery = nil
			tc.args[paramPrometheusURL] = mockURL
			result := callToolResult(t, dispatchToolCall(t, srv, "get_rules", tc.args))
			if !result.IsError {
				t.Fatalf("expected validation to reject %v", tc.args)
			}
			if text := resultText(result); !strings.Contains(text, tc.want) {
				t.Errorf("validation error should mention %q; got: %s", tc.want, text)
			}
			if gotQuery != nil {
				t.Errorf("rejected call must not reach Prometheus, but it sent %v", gotQuery)
			}
		})
	}
}
