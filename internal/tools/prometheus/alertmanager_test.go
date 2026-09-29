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

const (
	testTeamMatcher    = `team="bumblebee"`
	testPrometheusURL  = "http://prometheus:9090"
	testAlertmanagerAt = "/alertmanager"
)

// fakeAlertmanagerBody is a GET /api/v2/alerts answer, newest first, the way
// Alertmanager does not guarantee any order.
const fakeAlertmanagerBody = `[
  {"fingerprint":"b2","startsAt":"2026-09-29T02:00:00Z","status":{"state":"active"},
   "labels":{"alertname":"KagentAgentNamespaceQuotaNearlyFull","severity":"notify","team":"bumblebee"},
   "annotations":{"description":"quota nearly full"},"receivers":[{"name":"team_slack"}]},
  {"fingerprint":"a1","startsAt":"2026-09-29T01:00:00Z","status":{"state":"active"},
   "labels":{"alertname":"MCPKubernetesDown","severity":"page","team":"bumblebee"},
   "receivers":[{"name":"pager"},{"name":"team_slack"}]}
]`

// fakeAlertmanager serves fakeAlertmanagerBody under prefix and records the
// last request.
func fakeAlertmanager(t *testing.T, prefix string, last **http.Request) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*last = r
		if r.URL.Path != prefix+alertmanagerAlertsEndpoint {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.URL.Query().Get(paramFilter) == "bad" {
			http.Error(w, `bad matcher format: bad`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fakeAlertmanagerBody))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGetAlertmanagerAlerts(t *testing.T) {
	var last *http.Request
	srv := fakeAlertmanager(t, testAlertmanagerAt, &last)

	client, err := NewClient(server.PrometheusConfig{URL: srv.URL + testAlertmanagerAt, OrgID: "giantswarm"}, discardLogger())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	alerts, err := client.GetAlertmanagerAlerts(context.Background(), AlertmanagerAlertsOptions{
		Receiver: "team_.*",
		Filter:   []string{testTeamMatcher, `severity=~"page|notify"`},
	})
	if err != nil {
		t.Fatalf("GetAlertmanagerAlerts: %v", err)
	}

	want := url.Values{
		"active":      {"true"},
		"silenced":    {"false"},
		"inhibited":   {"false"},
		paramReceiver: {"team_.*"},
		paramFilter:   {testTeamMatcher, `severity=~"page|notify"`},
	}
	if got := last.URL.Query(); !reflect.DeepEqual(got, want) {
		t.Errorf("query = %v, want %v", got, want)
	}
	if got := last.Header.Get("X-Scope-OrgID"); got != "giantswarm" {
		t.Errorf("X-Scope-OrgID = %q, want giantswarm", got)
	}

	if len(alerts) != 2 {
		t.Fatalf("got %d alerts, want 2", len(alerts))
	}
	first := alerts[0]
	if first.Fingerprint != "a1" || first.Alertname != "MCPKubernetesDown" || first.Severity != "page" {
		t.Errorf("first alert = %+v, want the oldest (a1, MCPKubernetesDown, page)", first)
	}
	if want := []string{"pager", "team_slack"}; !reflect.DeepEqual(first.Receivers, want) {
		t.Errorf("receivers = %v, want %v", first.Receivers, want)
	}
	if alerts[1].Annotations["description"] != "quota nearly full" {
		t.Errorf("annotations = %v", alerts[1].Annotations)
	}
}

func TestGetAlertmanagerAlertsSurfacesError(t *testing.T) {
	var last *http.Request
	srv := fakeAlertmanager(t, "", &last)

	client, err := NewClient(server.PrometheusConfig{URL: srv.URL}, discardLogger())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.GetAlertmanagerAlerts(context.Background(), AlertmanagerAlertsOptions{Filter: []string{"bad"}})
	if err == nil || !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "bad matcher format") {
		t.Errorf("error = %v, want the status and the Alertmanager's message", err)
	}
}

// callTool dispatches one tools/call through the server and returns the result.
func callTool(t *testing.T, s *mcpserver.MCPServer, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	msg, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	resp, ok := s.HandleMessage(context.Background(), msg).(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("tools/call %s: not a result response", name)
	}
	res, ok := resp.Result.(*mcp.CallToolResult)
	if !ok {
		t.Fatalf("tools/call %s: result is %T", name, resp.Result)
	}
	return res
}

func TestHandleGetAlertmanagerAlerts(t *testing.T) {
	var last *http.Request
	srv := fakeAlertmanager(t, testAlertmanagerAt, &last)

	tests := []struct {
		name      string
		config    server.PrometheusConfig
		args      map[string]any
		wantError string
	}{
		{
			name:   "ALERTMANAGER_URL default",
			config: server.PrometheusConfig{URL: testPrometheusURL, AlertmanagerURL: srv.URL + testAlertmanagerAt},
			args:   map[string]any{paramFilter: []any{testTeamMatcher}},
		},
		{
			name:   "alertmanager_url parameter",
			config: server.PrometheusConfig{URL: testPrometheusURL},
			args:   map[string]any{paramAlertmanagerURL: srv.URL + testAlertmanagerAt},
		},
		{
			name:      "no Alertmanager configured",
			config:    server.PrometheusConfig{URL: testPrometheusURL},
			args:      map[string]any{},
			wantError: "ALERTMANAGER_URL is not set",
		},
		{
			name:      "link-local URL refused",
			config:    server.PrometheusConfig{URL: testPrometheusURL},
			args:      map[string]any{paramAlertmanagerURL: "http://169.254.169.254/"},
			wantError: "invalid alertmanager_url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc, err := server.NewServerContext(context.Background(),
				server.WithPrometheusConfig(tt.config), server.WithSlogLogger(discardLogger()))
			if err != nil {
				t.Fatalf("NewServerContext: %v", err)
			}
			defer func() { _ = sc.Shutdown() }()

			s := mcpserver.NewMCPServer("test", "1.0.0", mcpserver.WithToolCapabilities(true))
			if err := RegisterPrometheusTools(s, sc); err != nil {
				t.Fatalf("RegisterPrometheusTools: %v", err)
			}

			res := callTool(t, s, toolGetAlertmanagerAlerts, tt.args)
			text := res.Content[0].(mcp.TextContent).Text
			if tt.wantError != "" {
				if !res.IsError || !strings.Contains(text, tt.wantError) {
					t.Errorf("result = %q (isError %v), want an error containing %q", text, res.IsError, tt.wantError)
				}
				return
			}
			if res.IsError {
				t.Fatalf("unexpected error: %s", text)
			}
			var got struct {
				Count  int                 `json:"count"`
				Alerts []AlertmanagerAlert `json:"alerts"`
			}
			if err := json.Unmarshal([]byte(text), &got); err != nil {
				t.Fatalf("result is not JSON: %v\n%s", err, text)
			}
			if got.Count != 2 || got.Alerts[0].Fingerprint != "a1" {
				t.Errorf("result = %+v, want 2 alerts, oldest first", got)
			}
		})
	}
}
