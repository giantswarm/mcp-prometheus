package prometheus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
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
	return callToolWithContext(context.Background(), t, s, name, args)
}

// callToolWithContext is callTool with the caller's context, which carries
// its OAuth user info.
func callToolWithContext(ctx context.Context, t *testing.T, s *mcpserver.MCPServer, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	msg, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	resp, ok := s.HandleMessage(ctx, msg).(mcp.JSONRPCResponse)
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

// fakeDemoAlertmanagerBody is the demo tenant's GET /api/v2/alerts answer:
// one alert that starts between the two of fakeAlertmanagerBody.
const fakeDemoAlertmanagerBody = `[
  {"fingerprint":"c3","startsAt":"2026-09-29T01:30:00Z","status":{"state":"active"},
   "labels":{"alertname":"DemoAgentDown","severity":"notify","team":"demo"},
   "receivers":[{"name":"demo_slack"}]}
]`

// fakeMimirAlertmanager serves one body per X-Scope-OrgID the way Mimir's
// Alertmanager does: it refuses a pipe-separated selector with 401 "multiple
// org IDs present" and answers 404 for a tenant without a body. It records
// the org IDs read.
type fakeMimirAlertmanager struct {
	mu   sync.Mutex
	seen []string
}

func newFakeMimirAlertmanager(t *testing.T, bodies map[string]string) (*httptest.Server, *fakeMimirAlertmanager) {
	t.Helper()
	f := &fakeMimirAlertmanager{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orgID := r.Header.Get("X-Scope-OrgID")
		f.mu.Lock()
		f.seen = append(f.seen, orgID)
		f.mu.Unlock()
		if strings.Contains(orgID, "|") {
			http.Error(w, "multiple org IDs present", http.StatusUnauthorized)
			return
		}
		body, ok := bodies[orgID]
		if !ok {
			http.Error(w, "the Alertmanager is not configured for this tenant", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, f
}

// orgIDs is the org IDs read so far, sorted.
func (f *fakeMimirAlertmanager) orgIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := slices.Clone(f.seen)
	slices.Sort(out)
	return out
}

// TestHandleGetAlertmanagerAlertsTenants reads a Mimir Alertmanager under
// OAuth tenancy: the caller holds tenants, of which alerting carry alerting.
func TestHandleGetAlertmanagerAlertsTenants(t *testing.T) {
	const tenantUnconfigured = "unconfigured"
	// The tenant each fake alert is read from.
	alertTenants := map[string]string{"a1": testTenant, "b2": testTenant, "c3": tenantDemo}

	tests := []struct {
		name        string
		tenants     []string
		alerting    []string
		args        map[string]any
		wantRead    []string // org IDs the Alertmanager saw, sorted
		wantTenants []string // the result's tenants
		wantAlerts  []string // fingerprints, oldest first
		wantError   string
	}{
		{
			name:        "one alerting tenant",
			tenants:     []string{testTenant},
			alerting:    []string{testTenant},
			wantRead:    []string{testTenant},
			wantTenants: []string{testTenant},
			wantAlerts:  []string{"a1", "b2"},
		},
		{
			name:        "a data-only tenant never joins the org id",
			tenants:     []string{testTenant, tenantTraces},
			alerting:    []string{testTenant},
			wantRead:    []string{testTenant},
			wantTenants: []string{testTenant},
			wantAlerts:  []string{"a1", "b2"},
		},
		{
			name:        "several alerting tenants are read and merged",
			tenants:     []string{tenantDemo, testTenant, tenantTraces},
			alerting:    []string{tenantDemo, testTenant},
			wantRead:    []string{tenantDemo, testTenant},
			wantTenants: []string{tenantDemo, testTenant},
			wantAlerts:  []string{"a1", "c3", "b2"},
		},
		{
			name:        "org_id narrows the read",
			tenants:     []string{tenantDemo, testTenant},
			alerting:    []string{tenantDemo, testTenant},
			args:        map[string]any{paramOrgID: tenantDemo},
			wantRead:    []string{tenantDemo},
			wantTenants: []string{tenantDemo},
			wantAlerts:  []string{"c3"},
		},
		{
			name:      "org_id of a data-only tenant is refused, the alerting tenants named",
			tenants:   []string{tenantDemo, testTenant, tenantTraces},
			alerting:  []string{tenantDemo, testTenant},
			args:      map[string]any{paramOrgID: tenantTraces},
			wantError: "(" + tenantDemo + ", " + testTenant + ")",
		},
		{
			name:      "no alerting tenant",
			tenants:   []string{tenantTraces},
			wantError: "carries alerting",
		},
		{
			name:      "one tenant's failure names it",
			tenants:   []string{testTenant, tenantUnconfigured},
			alerting:  []string{testTenant, tenantUnconfigured},
			wantError: "tenant " + tenantUnconfigured + ": ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, fake := newFakeMimirAlertmanager(t, map[string]string{testTenant: fakeAlertmanagerBody, tenantDemo: fakeDemoAlertmanagerBody})
			sc := newTenancyTestContext(t,
				server.WithPrometheusConfig(server.PrometheusConfig{URL: testPrometheusURL, AlertmanagerURL: srv.URL}),
				server.WithOAuthEnabled(true),
				server.WithTenancyResolver(&stubTenancyResolver{tenants: tt.tenants, alerting: tt.alerting}),
			)
			s := mcpserver.NewMCPServer("test", "1.0.0", mcpserver.WithToolCapabilities(true))
			if err := RegisterPrometheusTools(s, sc); err != nil {
				t.Fatalf("RegisterPrometheusTools: %v", err)
			}
			args := tt.args
			if args == nil {
				args = map[string]any{}
			}

			res := callToolWithContext(userContext(), t, s, toolGetAlertmanagerAlerts, args)
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
				Count   int                 `json:"count"`
				Tenants []string            `json:"tenants"`
				Alerts  []AlertmanagerAlert `json:"alerts"`
			}
			if err := json.Unmarshal([]byte(text), &got); err != nil {
				t.Fatalf("result is not JSON: %v\n%s", err, text)
			}
			if read := fake.orgIDs(); !reflect.DeepEqual(read, tt.wantRead) {
				t.Errorf("Alertmanager read with X-Scope-OrgID %v, want %v", read, tt.wantRead)
			}
			if !reflect.DeepEqual(got.Tenants, tt.wantTenants) {
				t.Errorf("tenants = %v, want %v", got.Tenants, tt.wantTenants)
			}
			fingerprints := make([]string, 0, len(got.Alerts))
			for _, a := range got.Alerts {
				fingerprints = append(fingerprints, a.Fingerprint)
				if a.Tenant != alertTenants[a.Fingerprint] {
					t.Errorf("alert %s: tenant = %q, want %q", a.Fingerprint, a.Tenant, alertTenants[a.Fingerprint])
				}
			}
			if got.Count != len(tt.wantAlerts) || !reflect.DeepEqual(fingerprints, tt.wantAlerts) {
				t.Errorf("alerts = %v (count %d), want %v", fingerprints, got.Count, tt.wantAlerts)
			}
		})
	}
}
