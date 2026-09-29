package prometheus

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/giantswarm/mcp-prometheus/internal/server"
)

// alertmanagerAlertsEndpoint is the Alertmanager v2 API path for alerts,
// relative to the Alertmanager base URL (on Mimir: <gateway>/alertmanager).
const alertmanagerAlertsEndpoint = "/api/v2/alerts"

// get_alertmanager_alerts parameters.
const (
	toolGetAlertmanagerAlerts = "get_alertmanager_alerts"

	paramAlertmanagerURL = "alertmanager_url"
	paramReceiver        = "receiver"
	paramFilter          = "filter"
)

// alertmanagerAdvice is appended when get_alertmanager_alerts output is
// truncated. The Alertmanager API filters server-side, so the advice points
// the caller at the tool's own filters.
const alertmanagerAdvice = `

⚠️  RESULT TRUNCATED: The response exceeded 50k characters.

💡 To narrow the result, filter on the Alertmanager side:
   • "filter" with label matchers, e.g. ["team=\"bumblebee\"", "severity=\"page\""]
   • "receiver" with a receiver name or regex, e.g. "pager"`

// AlertmanagerAlertsOptions narrows a GetAlertmanagerAlerts call. Both fields
// map onto query parameters of GET /api/v2/alerts and are applied by the
// Alertmanager; zero values send nothing.
type AlertmanagerAlertsOptions struct {
	// Receiver keeps only alerts routed to a receiver matching this regex.
	Receiver string
	// Filter keeps only alerts matching every one of these label matchers
	// (e.g. `team="bumblebee"`, `severity=~"page|notify"`).
	Filter []string
}

// queryValues encodes the options as /api/v2/alerts query parameters. The
// result is always the alerts that notify: active, neither silenced nor
// inhibited.
func (o AlertmanagerAlertsOptions) queryValues() url.Values {
	q := url.Values{}
	q.Set("active", "true")
	q.Set("silenced", "false")
	q.Set("inhibited", "false")
	if o.Receiver != "" {
		q.Set(paramReceiver, o.Receiver)
	}
	for _, f := range o.Filter {
		q.Add(paramFilter, f)
	}
	return q
}

// AlertmanagerAlert is one alert as get_alertmanager_alerts returns it.
type AlertmanagerAlert struct {
	Fingerprint string            `json:"fingerprint"`
	Alertname   string            `json:"alertname,omitempty"`
	Severity    string            `json:"severity,omitempty"`
	StartsAt    time.Time         `json:"startsAt"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Receivers   []string          `json:"receivers,omitempty"`
}

// GetAlertmanagerAlerts returns the active alerts that are neither silenced
// nor inhibited, oldest first. The client's URL is the Alertmanager base URL;
// authentication, TLS and X-Scope-OrgID come from the same round trippers as
// every Prometheus call, so Mimir's multi-tenant Alertmanager works as is.
func (c *Client) GetAlertmanagerAlerts(ctx context.Context, options AlertmanagerAlertsOptions) ([]AlertmanagerAlert, error) {
	if c.apiClient == nil {
		return nil, fmt.Errorf("alertmanager client not initialized")
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	u := c.apiClient.URL(alertmanagerAlertsEndpoint, nil)
	u.RawQuery = options.queryValues().Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create alertmanager request: %w", err)
	}

	resp, body, err := c.apiClient.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get alertmanager alerts: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("failed to get alertmanager alerts: %s: %s", resp.Status, truncateBody(body))
	}

	var raw []struct {
		Fingerprint string            `json:"fingerprint"`
		StartsAt    time.Time         `json:"startsAt"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
		Receivers   []struct {
			Name string `json:"name"`
		} `json:"receivers"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to decode alertmanager alerts: %w", err)
	}

	alerts := make([]AlertmanagerAlert, 0, len(raw))
	for _, r := range raw {
		a := AlertmanagerAlert{
			Fingerprint: r.Fingerprint,
			Alertname:   r.Labels["alertname"],
			Severity:    r.Labels["severity"],
			StartsAt:    r.StartsAt,
			Labels:      r.Labels,
			Annotations: r.Annotations,
		}
		for _, rc := range r.Receivers {
			a.Receivers = append(a.Receivers, rc.Name)
		}
		alerts = append(alerts, a)
	}
	slices.SortFunc(alerts, func(a, b AlertmanagerAlert) int {
		return cmp.Or(a.StartsAt.Compare(b.StartsAt), cmp.Compare(a.Fingerprint, b.Fingerprint))
	})
	return alerts, nil
}

// truncateBody bounds an error body quoted in an error message.
func truncateBody(body []byte) string {
	const maxLen = 512
	if len(body) > maxLen {
		return string(body[:maxLen]) + "…"
	}
	return string(body)
}

// createAlertmanagerClient builds a client for the Alertmanager named by the
// alertmanager_url parameter or ALERTMANAGER_URL. It inherits the Prometheus
// authentication and TLS settings and resolves the tenant exactly like the
// Prometheus tools.
func createAlertmanagerClient(ctx context.Context, params map[string]any, sc *server.ServerContext) (*Client, error) {
	config := sc.PrometheusConfig()

	if u := getStringParam(params, paramAlertmanagerURL); u != "" {
		if err := validateURL(paramAlertmanagerURL, u); err != nil {
			return nil, err
		}
		config.AlertmanagerURL = u
	}
	if config.AlertmanagerURL == "" {
		return nil, fmt.Errorf("%s parameter is required (ALERTMANAGER_URL is not set)", paramAlertmanagerURL)
	}
	config.URL = config.AlertmanagerURL

	orgID, err := resolveTenantOrgID(ctx, sc, getStringParam(params, "org_id"))
	if err != nil {
		return nil, err
	}
	if orgID != "" {
		config.OrgID = orgID
	}

	return NewClient(config, sc.Logger())
}

// handleGetAlertmanagerAlerts handles the get_alertmanager_alerts tool.
func handleGetAlertmanagerAlerts(ctx context.Context, request mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	params := extractParams(request)

	client, err := createAlertmanagerClient(ctx, params, sc)
	if err != nil {
		return errorResult(fmt.Sprintf("Error creating Alertmanager client: %v", err)), nil
	}

	alerts, err := client.GetAlertmanagerAlerts(ctx, AlertmanagerAlertsOptions{
		Receiver: getStringParam(params, paramReceiver),
		Filter:   extractStringArray(params, paramFilter),
	})
	if err != nil {
		sc.Logger().Error("Failed to get alertmanager alerts", "error", err)
		return errorResult(fmt.Sprintf("Error getting Alertmanager alerts: %v", err)), nil
	}

	out, err := json.Marshal(struct {
		Count  int                 `json:"count"`
		Alerts []AlertmanagerAlert `json:"alerts"`
	}{len(alerts), alerts})
	if err != nil {
		return errorResult(fmt.Sprintf("Error encoding Alertmanager alerts: %v", err)), nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.TextContent{Type: contentTypeText, Text: string(out)}},
	}, nil
}

// errorResult is a tool result that reports an error to the caller.
func errorResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{mcp.TextContent{Type: contentTypeText, Text: text}},
	}
}
