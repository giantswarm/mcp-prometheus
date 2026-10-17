package prometheus

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"golang.org/x/sync/errgroup"

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
	paramAlertsLimit     = "limit"
)

// alertmanagerNarrowing names the filters that narrow a truncated
// get_alertmanager_alerts answer. The Alertmanager filters server-side; the
// answer already holds only the alerts that notify.
const alertmanagerNarrowing = `Alerts are oldest first; the newest were left out. Narrow with "filter" label matchers ` +
	`(e.g. ["team=\"bumblebee\"", "severity=\"page\""]), "receiver" (a receiver regex), "org_id" (one tenant) ` +
	`or "limit". Only active alerts that are neither silenced nor inhibited are listed.`

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
	Fingerprint string `json:"fingerprint"`
	// Tenant is the X-Scope-OrgID the alert was read with, when there is one.
	Tenant      string            `json:"tenant,omitempty"`
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
	sortAlertmanagerAlerts(alerts)
	return alerts, nil
}

// sortAlertmanagerAlerts orders alerts oldest first, ties by fingerprint,
// then by tenant.
func sortAlertmanagerAlerts(alerts []AlertmanagerAlert) {
	slices.SortFunc(alerts, func(a, b AlertmanagerAlert) int {
		return cmp.Or(a.StartsAt.Compare(b.StartsAt), cmp.Compare(a.Fingerprint, b.Fingerprint), cmp.Compare(a.Tenant, b.Tenant))
	})
}

// truncateBody bounds an error body quoted in an error message.
func truncateBody(body []byte) string {
	const maxLen = 512
	if len(body) > maxLen {
		return string(body[:maxLen]) + "…"
	}
	return string(body)
}

// alertmanagerConfig is the client configuration for the Alertmanager named
// by the alertmanager_url parameter or ALERTMANAGER_URL. It inherits the
// Prometheus authentication, TLS and default tenant settings.
func alertmanagerConfig(params map[string]any, sc *server.ServerContext) (server.PrometheusConfig, error) {
	config := sc.PrometheusConfig()

	if u := getStringParam(params, paramAlertmanagerURL); u != "" {
		if err := validateURL(paramAlertmanagerURL, u); err != nil {
			return server.PrometheusConfig{}, err
		}
		config.AlertmanagerURL = u
	}
	if config.AlertmanagerURL == "" {
		return server.PrometheusConfig{}, fmt.Errorf("%s parameter is required (ALERTMANAGER_URL is not set)", paramAlertmanagerURL)
	}
	config.URL = config.AlertmanagerURL
	return config, nil
}

// tenantsToRead is the X-Scope-OrgID value for each selected org ID, the
// configured default tenant standing in for an empty one ("" when there is
// none either: the Alertmanager is read without a tenant).
func tenantsToRead(orgIDs []string, defaultOrgID string) []string {
	tenants := make([]string, len(orgIDs))
	for i, id := range orgIDs {
		tenants[i] = cmp.Or(id, defaultOrgID)
	}
	return tenants
}

// getAlertmanagerAlertsForTenants reads the Alertmanager of every tenant
// concurrently, a client per tenant (a configuration copy and its round
// trippers, no connection of its own), and merges the alerts, each marked with
// its tenant, oldest first. Mimir's Alertmanager serves one tenant per
// request, so this is how several tenants are read at all. One tenant's
// failure fails the call and names the tenant: a partial alert list would
// hide an outage.
func getAlertmanagerAlertsForTenants(ctx context.Context, config server.PrometheusConfig, tenants []string, options AlertmanagerAlertsOptions, logger *slog.Logger) ([]AlertmanagerAlert, error) {
	results := make([][]AlertmanagerAlert, len(tenants))
	g, ctx := errgroup.WithContext(ctx)
	for i, tenant := range tenants {
		g.Go(func() error {
			cfg := config
			cfg.OrgID = tenant
			client, err := NewClient(cfg, logger)
			if err != nil {
				return tenantError(tenant, err)
			}
			alerts, err := client.GetAlertmanagerAlerts(ctx, options)
			if err != nil {
				return tenantError(tenant, err)
			}
			for j := range alerts {
				alerts[j].Tenant = tenant
			}
			results[i] = alerts
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	merged := make([]AlertmanagerAlert, 0)
	for _, alerts := range results {
		merged = append(merged, alerts...)
	}
	sortAlertmanagerAlerts(merged)
	return merged, nil
}

// tenantError names the tenant an Alertmanager call failed for.
func tenantError(tenant string, err error) error {
	if tenant == "" {
		return err
	}
	return fmt.Errorf("tenant %s: %w", tenant, err)
}

// handleGetAlertmanagerAlerts handles the get_alertmanager_alerts tool.
func handleGetAlertmanagerAlerts(ctx context.Context, request mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	params := extractParams(request)

	config, err := alertmanagerConfig(params, sc)
	if err != nil {
		return errorResult(fmt.Sprintf("Error creating Alertmanager client: %v", err)), nil
	}
	orgIDs, err := resolveAlertingOrgIDs(ctx, sc, getStringParam(params, paramOrgID))
	if err != nil {
		return errorResult(fmt.Sprintf("Error creating Alertmanager client: %v", err)), nil
	}
	tenants := tenantsToRead(orgIDs, config.OrgID)

	alerts, err := getAlertmanagerAlertsForTenants(ctx, config, tenants, AlertmanagerAlertsOptions{
		Receiver: getStringParam(params, paramReceiver),
		Filter:   extractStringArray(params, paramFilter),
	}, sc.Logger())
	if err != nil {
		sc.Logger().Error("Failed to get alertmanager alerts", "error", err)
		return errorResult(fmt.Sprintf("Error getting Alertmanager alerts: %v", err)), nil
	}

	limit, err := getAlertsLimit(params)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	out, err := encodeAlertmanagerAnswer(alerts, slices.DeleteFunc(tenants, func(t string) bool { return t == "" }), limit, MaxResultLength)
	if err != nil {
		return errorResult(fmt.Sprintf("Error encoding Alertmanager alerts: %v", err)), nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.TextContent{Type: contentTypeText, Text: string(out)}},
	}, nil
}

// alertmanagerAnswer is the get_alertmanager_alerts result: Count is the
// number of alerts in Alerts, Truncated is set when alerts were left out.
type alertmanagerAnswer struct {
	Count     int               `json:"count"`
	Tenants   []string          `json:"tenants,omitempty"`
	Alerts    []json.RawMessage `json:"alerts"`
	Truncated *alertsTruncation `json:"truncated,omitempty"`
}

// alertsTruncation tells the caller how many alerts an answer left out, why,
// and how to narrow the read.
type alertsTruncation struct {
	Total   int    `json:"total"`
	Omitted int    `json:"omitted"`
	Reason  string `json:"reason"`
	Narrow  string `json:"narrow"`
}

// encodeAlertmanagerAnswer encodes as many complete alerts as limit (0 for
// none) and maxLen bytes allow, in order, so the answer stays valid JSON
// whatever its size; a cut answer carries a truncated marker.
func encodeAlertmanagerAnswer(alerts []AlertmanagerAlert, tenants []string, limit, maxLen int) ([]byte, error) {
	raw := make([]json.RawMessage, len(alerts))
	for i, a := range alerts {
		b, err := json.Marshal(a)
		if err != nil {
			return nil, err
		}
		raw[i] = b
	}

	encode := func(kept int, reason string) ([]byte, error) {
		answer := alertmanagerAnswer{Count: kept, Tenants: tenants, Alerts: raw[:kept]}
		if kept < len(raw) {
			answer.Truncated = &alertsTruncation{Total: len(raw), Omitted: len(raw) - kept, Reason: reason, Narrow: alertmanagerNarrowing}
		}
		return json.Marshal(answer)
	}

	kept, reason := len(raw), ""
	if limit > 0 && limit < kept {
		kept, reason = limit, fmt.Sprintf("limit %d", limit)
	}
	out, err := encode(kept, reason)
	if err != nil || len(out) <= maxLen {
		return out, err
	}

	// The answer grows with every alert kept: the largest prefix that fits is
	// found by bisection, each candidate encoded and measured. Zero alerts
	// always fit.
	reason = fmt.Sprintf("answer over %d characters", maxLen)
	fits := 0
	for lo, hi := 1, kept-1; lo <= hi; {
		mid := lo + (hi-lo)/2
		b, err := encode(mid, reason)
		if err != nil {
			return nil, err
		}
		if len(b) <= maxLen {
			fits, out, lo = mid, b, mid+1
		} else {
			hi = mid - 1
		}
	}
	if fits == 0 {
		return encode(0, reason)
	}
	return out, nil
}

// getAlertsLimit returns the limit parameter, 0 when absent.
func getAlertsLimit(params map[string]any) (int, error) {
	v, ok := params[paramAlertsLimit]
	if !ok || v == nil {
		return 0, nil
	}
	n, ok := v.(float64)
	if !ok || n < 1 || n != float64(int(n)) {
		return 0, fmt.Errorf("%s must be a positive integer", paramAlertsLimit)
	}
	return int(n), nil
}

// errorResult is a tool result that reports an error to the caller.
func errorResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{mcp.TextContent{Type: contentTypeText, Text: text}},
	}
}
