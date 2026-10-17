package prometheus

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/giantswarm/mcp-oauth/handler"
	"github.com/giantswarm/mcp-toolkit/tooldesc"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/mcp-prometheus/internal/server"
	"github.com/giantswarm/mcp-prometheus/internal/tenancy"
)

// Constants for result truncation
const (
	MaxResultLength  = 50000
	TruncationAdvice = `

⚠️  RESULT TRUNCATED: The query returned a very large result (>50k characters).

💡 To optimize your query and get less output, consider:
   • Adding more specific label filters: {app="specific-app", namespace="specific-ns"}
   • Using aggregation functions: sum(), avg(), count(), etc.
   • Limiting time ranges for range queries
   • Using topk() or bottomk() to get only top/bottom N results
   • Filtering by specific metrics instead of using wildcards

🔧 To get the full untruncated result, add "unlimited": "true" to your query parameters, but be aware this may impact performance.`

	// discoveryAdvice is appended when label/series/metadata/exemplar tool
	// output is truncated. These tools accept matchers, time windows and
	// limits, so the advice nudges the caller toward narrower requests.
	discoveryAdvice = `

⚠️  RESULT TRUNCATED: The response exceeded 50k characters.

💡 To get a smaller, more focused result, consider:
   • Passing a tighter "matches" selector (e.g. {namespace="my-ns", job="my-job"})
   • Narrowing the "start_time" / "end_time" window
   • Setting an explicit "limit" parameter
   • Querying a specific metric name instead of broad patterns`

	// alertsAdvice is appended when get_alerts output is truncated. The
	// ALERTS series is queryable via PromQL, so the advice points the caller
	// at execute_query with a narrower selector.
	alertsAdvice = `

⚠️  RESULT TRUNCATED: The response exceeded 50k characters.

💡 To narrow the result, query the ALERTS series directly via "execute_query":
   • execute_query with ALERTS{alertname="..."} to inspect a specific alert
   • execute_query with ALERTS{severity="critical"} to filter by label
   • Combine with topk() / count() to summarise rather than enumerate`

	// bulkAdvice is appended when tools that return server-wide state are
	// truncated and there is no narrower API on the Prometheus/Mimir side
	// (get_rules, get_targets, get_config, get_tsdb_stats). The honest answer
	// is "fetch less or filter on the client side."
	bulkAdvice = `

⚠️  RESULT TRUNCATED: The response exceeded 50k characters.

💡 This tool returns the full server-side state and has no narrower API. Options:
   • If the tool exposes a "limit" parameter, pass one
   • Re-run against a more focused tenant/org scope
   • Fetch less often and filter or paginate on the client side`

	// noTruncation, when passed as the advice argument to
	// registerPrometheusTools, disables the truncation middleware for that
	// tool. Use it for tools whose output is bounded in practice
	// (get_build_info, get_flags, …).
	noTruncation = ""

	// contentTypeText is the discriminator value used in mcp.TextContent.Type
	// for plain-text MCP tool responses.
	contentTypeText = "text"

	// toolExecuteQuery is the registered name of the PromQL instant-query tool.
	toolExecuteQuery = "execute_query"

	// toolExecuteRangeQuery is the registered name of the PromQL range-query tool.
	toolExecuteRangeQuery = "execute_range_query"

	// toolGetAlerts and toolGetRules are the registered names of the ruler
	// tools, which address one tenant per request (see tenantScopeOf).
	toolGetAlerts = "get_alerts"
	toolGetRules  = "get_rules"

	// rulerTenantDetails ends the ruler tools' description details: Mimir's
	// ruler API serves one tenant per request, and the refusal names the
	// caller's tenants when org_id is missing.
	rulerTenantDetails = "On Mimir it reads one tenant per call, chosen with org_id."

	// errQueryParameterRequired is returned when a query handler is called without
	// the required "query" argument.
	errQueryParameterRequired = "Error: query parameter is required and must be a string"

	// paramOrgID is the tenant parameter (X-Scope-OrgID) of every tool.
	paramOrgID = "org_id"

	// get_rules filter parameters (see RulesOptions for the wire mapping).
	paramRuleType      = "type"
	paramRuleName      = "rule_name"
	paramRuleGroup     = "rule_group"
	paramRuleFile      = "file"
	paramExcludeAlerts = "exclude_alerts"
)

// Common parameter builders to reduce repetition
func withPrometheusConnectionParams(options ...mcp.ToolOption) []mcp.ToolOption {
	connectionParams := []mcp.ToolOption{
		mcp.WithString("prometheus_url",
			mcp.Description("Prometheus or Mimir URL, if not the configured one"),
		),
		mcp.WithString(paramOrgID,
			mcp.Description("Tenant (X-Scope-OrgID)"),
		),
	}
	return append(connectionParams, options...)
}

func withQueryEnhancementParams(options ...mcp.ToolOption) []mcp.ToolOption {
	enhancementParams := []mcp.ToolOption{
		mcp.WithString("timeout",
			mcp.Description("Query timeout (e.g., '30s', '1m', '5m')"),
		),
		mcp.WithString("limit",
			mcp.Description("Maximum entries returned"),
		),
		mcp.WithString("stats",
			mcp.Description("Include query statistics: 'all'"),
		),
		mcp.WithString("lookback_delta",
			mcp.Description("Query lookback delta (e.g., '5m')"),
		),
		mcp.WithString("unlimited",
			mcp.Description("'true' lifts the 50k-character output cap: the result may be huge"),
		),
	}
	return append(enhancementParams, options...)
}

func withTimeFilteringParams(options ...mcp.ToolOption) []mcp.ToolOption {
	timeParams := []mcp.ToolOption{
		mcp.WithString("start_time",
			mcp.Description("Start time (RFC3339)"),
		),
		mcp.WithString("end_time",
			mcp.Description("End time (RFC3339)"),
		),
	}
	return append(timeParams, options...)
}

func withLabelMatchingParams(options ...mcp.ToolOption) []mcp.ToolOption {
	matchParams := []mcp.ToolOption{
		mcp.WithArray("matches",
			mcp.Description("Series selectors to filter by"),
		),
	}
	return append(matchParams, options...)
}

// ToolMiddleware is an optional hook invoked around each MCP tool call.
// name is the tool name; next is the underlying handler.
// Implement this to add metrics, tracing, or other cross-cutting concerns.
type ToolMiddleware func(
	name string,
	next func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error),
) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)

// allowsUnlimited reports whether the named tool honours the
// "unlimited": "true" request argument. The set is closed and static —
// limiting it prevents callers who learned the trick from execute_query's
// description from silently disabling the cap on bulk-dump tools that have
// no other safety valve.
func allowsUnlimited(name string) bool {
	switch name {
	case toolExecuteQuery, toolExecuteRangeQuery:
		return true
	}
	return false
}

// tenantScope names the tenants a tool may address under OAuth tenancy.
type tenantScope int

const (
	// scopeData addresses every tenant of the caller; Mimir's querier
	// federates them in one request ("a|b").
	scopeData tenantScope = iota
	// scopeAlerting addresses the caller's tenants of type alerting; Mimir's
	// ruler and Alertmanager serve one tenant per request.
	scopeAlerting
)

// tenantScopeOf is the scope of the named tool. The set is closed and static
// like allowsUnlimited's: the ruler tools address alerting tenants, every
// other Prometheus tool the querier's.
func tenantScopeOf(name string) tenantScope {
	switch name {
	case toolGetAlerts, toolGetRules:
		return scopeAlerting
	}
	return scopeData
}

// truncationMiddleware caps oversized TextContent in tool results at
// MaxResultLength and appends the given advice. It is wired by
// registerPrometheusTools for every tool whose advice argument is non-empty.
//
// Honours the "unlimited": "true" request argument only on the tools that
// allowsUnlimited returns true for; other tools cannot opt out of truncation.
func truncationMiddleware(
	name, advice string,
	next func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error),
) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := next(ctx, req)
		if err != nil || res == nil {
			return res, err
		}
		if allowsUnlimited(name) && isUnlimitedRequest(req) {
			return res, nil
		}
		// res.Content is freshly allocated by every handler in this package, so
		// in-place mutation is safe. If a future handler ever returns a shared
		// slice, clone here before writing.
		for i, c := range res.Content {
			tc, ok := c.(mcp.TextContent)
			if !ok || len(tc.Text) <= MaxResultLength {
				continue
			}
			tc.Text = truncateWithAdvice(tc.Text, advice)
			res.Content[i] = tc
		}
		return res, nil
	}
}

// isUnlimitedRequest reports whether the caller passed "unlimited": "true"
// in the tool arguments. Whether the bypass is honoured depends on the tool;
// see allowsUnlimited.
func isUnlimitedRequest(req mcp.CallToolRequest) bool {
	args, ok := req.Params.Arguments.(map[string]any)
	if !ok {
		return false
	}
	v, _ := args["unlimited"].(string)
	return v == "true"
}

// Handler wrapper type for cleaner function signatures
type PrometheusHandler func(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error)

// Wrapper that handles dynamic client creation and error handling; scope
// names the tenants the tool may address.
func withDynamicPrometheusClient(handler PrometheusHandler, client *Client, sc *server.ServerContext, scope tenantScope) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := extractParams(request)

		// Create client with dynamic parameters if provided
		dynamicClient, err := createClientFromParams(ctx, params, client, sc, scope)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{
					mcp.TextContent{
						Type: contentTypeText,
						Text: fmt.Sprintf("Error creating Prometheus client: %v", err),
					},
				},
			}, nil
		}

		// Call the actual handler with the dynamic client
		return handler(ctx, request, dynamicClient, sc)
	}
}

// Helper function to create and register a tool with common patterns.
func registerPrometheusTools(s *mcpserver.MCPServer, client *Client, sc *server.ServerContext, middleware []ToolMiddleware, toolName string, description mcp.ToolOption, advice string, handler PrometheusHandler, options ...mcp.ToolOption) {
	registerTool(s, middleware, toolName, description, advice,
		withDynamicPrometheusClient(handler, client, sc, tenantScopeOf(toolName)), withPrometheusConnectionParams(options...)...)
}

// registerTool registers one read-only tool with the truncation middleware
// (unless advice is noTruncation) and the user-supplied middlewares.
// description is built with tooldesc.Describe: a summary line, a blank line
// and details, so muster's discovery shows the summary whole.
//
// All tools in this server are read-only (they query Prometheus, Mimir or
// Alertmanager without mutating state) and target a bounded endpoint rather
// than the open web. destructiveHint is set to false explicitly: mcp-go
// defaults it to true, which would report every read-only tool destructive.
func registerTool(s *mcpserver.MCPServer, middleware []ToolMiddleware, toolName string, description mcp.ToolOption, advice string, h func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), options ...mcp.ToolOption) {
	baseOptions := []mcp.ToolOption{
		description,
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		// Combined with WithInputSchemaValidation() (server-level), this rejects
		// unknown property names so typo'd args surface as errors — SEP-1303.
		mcp.WithSchemaAdditionalProperties(false),
	}
	tool := mcp.NewTool(toolName, append(baseOptions, options...)...)

	if advice != noTruncation {
		h = truncationMiddleware(toolName, advice, h)
	}
	// User-supplied middlewares wrap the (possibly already truncated) result,
	// so any telemetry middleware sees post-truncation byte counts.
	for _, mw := range middleware {
		h = mw(toolName, h)
	}
	s.AddTool(tool, h)
}

// RegisterPrometheusTools registers Prometheus-related tools with the MCP server.
// Pass optional ToolMiddleware values to instrument every tool call (e.g. metrics, tracing).
func RegisterPrometheusTools(s *mcpserver.MCPServer, sc *server.ServerContext, middleware ...ToolMiddleware) error {
	// Create a default Prometheus client if a URL is configured at startup.
	// PROMETHEUS_URL is optional — tools accept prometheus_url per-request instead.
	var client *Client
	if sc.PrometheusConfig().URL != "" {
		var err error
		client, err = NewClient(sc.PrometheusConfig(), sc.Logger())
		if err != nil {
			return fmt.Errorf("tools: create Prometheus client: %w", err)
		}
	}

	// Query execution tools
	registerPrometheusTools(s, client, sc, middleware, toolExecuteQuery, tooldesc.Describe("Run a PromQL instant query: the value of a metric or expression at one point in time.",
		"For values over a time window use execute_range_query; to find metric and label names use list_label_values."),
		TruncationAdvice, handleExecuteQuery, withQueryEnhancementParams(
			mcp.WithString("query", mcp.Required(), mcp.Description("PromQL query string")),
			mcp.WithString("time", mcp.Description("RFC3339 or Unix timestamp; default now")),
		)...)

	registerPrometheusTools(s, client, sc, middleware, toolExecuteRangeQuery, tooldesc.Describe("Run a PromQL range query: the values of a metric or expression over a time window, one sample per step.",
		"For a single point in time use execute_query."),
		TruncationAdvice, handleExecuteRangeQuery, withQueryEnhancementParams(
			mcp.WithString("query", mcp.Required(), mcp.Description("PromQL query string")),
			mcp.WithString("start", mcp.Required(), mcp.Description("Start time as RFC3339 or Unix timestamp")),
			mcp.WithString("end", mcp.Required(), mcp.Description("End time as RFC3339 or Unix timestamp")),
			mcp.WithString("step", mcp.Required(), mcp.Description("Query resolution step width (e.g., '15s', '1m', '1h')")),
		)...)

	// Metrics discovery tools
	registerPrometheusTools(s, client, sc, middleware, "get_metric_metadata", tooldesc.Describe("Get a metric's metadata: its type, help text and unit.",
		"For the metadata as each scrape target exposes it use get_targets_metadata."),
		discoveryAdvice, handleGetMetricMetadata,
		mcp.WithString("metric", mcp.Required(), mcp.Description("Metric name")),
		mcp.WithString("limit", mcp.Description("Maximum entries returned")),
	)

	// Label and series discovery tools
	registerPrometheusTools(s, client, sc, middleware, "list_label_names", tooldesc.Describe("List the label names of the stored series, optionally of those matching a selector.",
		"For one label's values use list_label_values."),
		discoveryAdvice, handleListLabelNames, withTimeFilteringParams(withLabelMatchingParams(
			mcp.WithString("limit", mcp.Description("Maximum entries returned")),
		)...)...)

	registerPrometheusTools(s, client, sc, middleware, "list_label_values", tooldesc.Describe("List the values of one label; the label __name__ lists the metric names.",
		"For the full label sets of matching series use find_series."),
		discoveryAdvice, handleListLabelValues, withTimeFilteringParams(withLabelMatchingParams(
			mcp.WithString("label", mcp.Required(), mcp.Description("Label name")),
			mcp.WithString("limit", mcp.Description("Maximum entries returned")),
		)...)...)

	registerPrometheusTools(s, client, sc, middleware, "find_series", tooldesc.Describe("Find time series by label matcher: the label set of every matching series.",
		"Returns labels, not samples: for values use execute_query."),
		discoveryAdvice, handleFindSeries, withTimeFilteringParams(
			mcp.WithArray("matches", mcp.Required(), mcp.Description("Array of label matchers (e.g., ['{job=\"prometheus\"}', '{__name__=~\"http_.*\"}'])")),
			mcp.WithString("limit", mcp.Description("Maximum entries returned")),
		)...)

	// Target and system information tools
	registerPrometheusTools(s, client, sc, middleware, "get_targets", tooldesc.Describe("List the scrape targets with their health, last scrape and last error, active and dropped.",
		"For the metrics a target exposes use get_targets_metadata."), bulkAdvice, handleGetTargets)

	registerPrometheusTools(s, client, sc, middleware, "get_build_info", tooldesc.Describe("Get the Prometheus server's build information: version, revision, branch and Go version.",
		"For runtime state use get_runtime_info."), noTruncation, handleGetBuildInfo)

	registerPrometheusTools(s, client, sc, middleware, "get_runtime_info", tooldesc.Describe("Get the Prometheus server's runtime information: start time, storage retention, config reload status.",
		"For the version use get_build_info."), noTruncation, handleGetRuntimeInfo)

	registerPrometheusTools(s, client, sc, middleware, "get_flags", tooldesc.Describe("Get the command-line flags the Prometheus server was started with.",
		"For the scrape and rule configuration use get_config."), noTruncation, handleGetFlags)

	registerPrometheusTools(s, client, sc, middleware, "get_config", tooldesc.Describe("Get the loaded Prometheus configuration file: scrape configs, rule files, remote write.",
		"For the live state of the scrape targets use get_targets."), bulkAdvice, handleGetConfig)

	// Alerting tools
	registerPrometheusTools(s, client, sc, middleware, toolGetAlerts,
		tooldesc.Describe("List the firing and pending alerts of the Prometheus or Mimir ruler.",
			"For the alerts that notify, not silenced or inhibited, use get_alertmanager_alerts. "+rulerTenantDetails),
		alertsAdvice, handleGetAlerts)

	registerPrometheusTools(s, client, sc, middleware, "get_alertmanagers", tooldesc.Describe("List the Alertmanager instances Prometheus discovered to send alerts to, active and dropped.",
		"For the alerts in Alertmanager use get_alertmanager_alerts."), noTruncation, handleGetAlertManagers)

	registerTool(s, middleware, toolGetAlertmanagerAlerts,
		tooldesc.Describe("List the alerts that notify from Alertmanager: active, not silenced or inhibited, with labels and receivers.",
			"For firing and pending alerts at the ruler use get_alerts. On Mimir every alerting tenant of the caller is read and merged; "+
				"org_id, pipe-separated for several, narrows the read. An answer over 50k characters keeps the oldest complete alerts "+
				"and a truncated marker with the omitted count."),
		noTruncation,
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return handleGetAlertmanagerAlerts(ctx, req, sc)
		},
		mcp.WithString(paramAlertmanagerURL,
			mcp.Description("Alertmanager base URL (e.g. 'http://mimir-gateway.mimir/alertmanager' or 'http://alertmanager:9093'); defaults to ALERTMANAGER_URL")),
		mcp.WithString(paramOrgID,
			mcp.Description("Tenant (X-Scope-OrgID) of the Mimir Alertmanager to read, pipe-separated for several; under OAuth one of the caller's alerting tenants")),
		mcp.WithString(paramReceiver, mcp.Description("Only alerts routed to a receiver matching this regex")),
		mcp.WithArray(paramFilter, mcp.WithStringItems(),
			mcp.Description("Only alerts matching every label matcher, e.g. ['team=\"bumblebee\"', 'severity=~\"page|notify\"']")),
		mcp.WithNumber(paramAlertsLimit, mcp.Min(1),
			mcp.Description("Maximum alerts returned, oldest first")),
	)

	registerPrometheusTools(s, client, sc, middleware, toolGetRules,
		tooldesc.Describe("List the alerting and recording rules of the Prometheus or Mimir ruler, filtered server-side.",
			"Prefer the filters and exclude_alerts over a full dump. "+rulerTenantDetails),
		bulkAdvice, handleGetRules,
		mcp.WithString(paramRuleType, mcp.Enum(RuleTypeAlert, RuleTypeRecord),
			mcp.Description("Return only alerting rules ('alert') or recording rules ('record'); omit for both")),
		mcp.WithArray(paramRuleName, mcp.WithStringItems(),
			mcp.Description("Only return rules with these exact names (e.g. ['MCPKubernetesDown'])")),
		mcp.WithArray(paramRuleGroup, mcp.WithStringItems(),
			mcp.Description("Only return rule groups with these exact names")),
		mcp.WithArray(paramRuleFile, mcp.WithStringItems(),
			mcp.Description("Only return rule groups loaded from these files; on Mimir the file is the rule namespace")),
		mcp.WithBoolean(paramExcludeAlerts,
			mcp.Description("Set to true to omit the active alerts embedded in alerting rules (much smaller output)")),
	)

	// Advanced tools
	registerPrometheusTools(s, client, sc, middleware, "get_tsdb_stats", tooldesc.Describe("Get TSDB cardinality statistics: series count and the metrics and labels with the most series.",
		"Use it to find what drives cardinality; for the series of one metric use find_series."),
		bulkAdvice, handleGetTSDBStats,
		mcp.WithString("limit", mcp.Description("Maximum entries returned")),
	)

	registerPrometheusTools(s, client, sc, middleware, "query_exemplars", tooldesc.Describe("Query exemplars for a PromQL selector: the trace IDs attached to its samples in a time window.",
		"For the metric values use execute_range_query."),
		discoveryAdvice, handleQueryExemplars,
		mcp.WithString("query", mcp.Required(), mcp.Description("PromQL selector")),
		mcp.WithString("start", mcp.Required(), mcp.Description("Start time as RFC3339 or Unix timestamp")),
		mcp.WithString("end", mcp.Required(), mcp.Description("End time as RFC3339 or Unix timestamp")),
	)

	registerPrometheusTools(s, client, sc, middleware, "get_targets_metadata", tooldesc.Describe("Get metric metadata per scrape target: type, help text and unit as each target exposes them.",
		"For one metric's metadata across targets use get_metric_metadata."),
		discoveryAdvice, handleGetTargetsMetadata,
		mcp.WithString("match_target", mcp.Description("Label selector of the targets")),
		mcp.WithString("metric", mcp.Description("Metric name")),
		mcp.WithString("limit", mcp.Description("Maximum entries returned")),
	)

	// Status / health tools
	registerPrometheusTools(s, client, sc, middleware, "check_ready", tooldesc.Describe("Check whether the Prometheus or Mimir server is ready to serve traffic.",
		"Use it when other tools fail to reach the server."), noTruncation, handleCheckReady)

	return nil
}

// truncateWithAdvice trims text to MaxResultLength and appends the given
// advice when truncation occurs. It tries to end at the last newline within
// the trailing 1000 bytes to avoid cutting mid-line, and backs off any
// half-rune at the cut so the result remains valid UTF-8 (the advice strings
// start with multi-byte characters, so a half-rune tail would corrupt the
// JSON encoding downstream).
func truncateWithAdvice(text, advice string) string {
	if len(text) <= MaxResultLength {
		return text
	}
	truncated := text[:MaxResultLength]
	if lastNewline := strings.LastIndex(truncated, "\n"); lastNewline > MaxResultLength-1000 {
		truncated = truncated[:lastNewline]
	} else {
		// No usable newline anchor — the byte cut may sit mid-rune. UTF-8
		// runes are at most 4 bytes, so up to 3 backoff steps suffice.
		for i := 0; i < utf8.UTFMax-1 && len(truncated) > 0 && !utf8.ValidString(truncated); i++ {
			truncated = truncated[:len(truncated)-1]
		}
	}
	return truncated + advice
}

// formatQueryResult formats the query result. When unlimited is set, a
// warning prefix is added; otherwise the raw formatted result is returned and
// truncationMiddleware applies the size cap downstream.
func formatQueryResult(resultType string, result any, unlimited bool) string {
	resultStr := fmt.Sprintf("Query executed successfully.\nResult Type: %s\nResult: %+v", resultType, result)
	if unlimited {
		return "⚠️  WARNING: Unlimited output enabled - this response may be very large and could impact performance.\n\n" + resultStr
	}
	return resultStr
}

// Helper function to extract parameters
func extractParams(request mcp.CallToolRequest) map[string]any {
	params := make(map[string]any)
	if request.Params.Arguments != nil {
		if argsMap, ok := request.Params.Arguments.(map[string]any); ok {
			params = argsMap
		}
	}
	return params
}

// Helper function to extract string array parameter
func extractStringArray(params map[string]any, key string) []string {
	if val, ok := params[key]; ok {
		if arr, ok := val.([]any); ok {
			result := make([]string, 0, len(arr))
			for _, item := range arr {
				if str, ok := item.(string); ok {
					result = append(result, str)
				}
			}
			return result
		}
	}
	return nil
}

// resolveTenantOrgID determines the effective X-Scope-OrgID for the request.
//
// When OAuth is enabled and a tenancy resolver is available it:
//  1. Extracts user groups from the token claims in ctx.
//  2. Resolves the allowed Mimir tenants via GrafanaOrganization CRDs.
//  3. Validates (or auto-injects) the org_id using [tenancy.SelectOrgID].
//
// When OAuth is disabled, the explicit override is returned verbatim (may be "").
func resolveTenantOrgID(ctx context.Context, sc *server.ServerContext, explicit string) (string, error) {
	if !sc.IsOAuthEnabled() {
		return explicit, nil
	}

	resolver := sc.TenancyResolver()
	if resolver == nil {
		return explicit, nil
	}

	userInfo, ok := handler.UserInfoFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("tenancy: no user info in context; token validation may have been skipped")
	}

	tenants, err := resolver.TenantsForGroups(ctx, userInfo.Groups)
	if err != nil {
		return "", fmt.Errorf("tenancy: resolve tenants: %w", err)
	}

	return tenancy.SelectOrgID(tenants, explicit)
}

// resolveAlertingOrgIDs determines the X-Scope-OrgID values for a request
// against an API that serves one tenant per request, Mimir's Alertmanager and
// ruler: one value per tenant to read.
//
// When OAuth is enabled and a tenancy resolver is available it resolves the
// caller's tenants of type alerting and validates (or selects all of) them
// with [tenancy.SelectOrgIDs]: a tenant that carries data only never reaches
// these APIs. Without OAuth or a resolver, the explicit value's pipe-separated
// parts are returned verbatim (a single "" when it is empty).
func resolveAlertingOrgIDs(ctx context.Context, sc *server.ServerContext, explicit string) ([]string, error) {
	if !sc.IsOAuthEnabled() || sc.TenancyResolver() == nil {
		return splitOrgID(explicit), nil
	}

	userInfo, ok := handler.UserInfoFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("tenancy: no user info in context; token validation may have been skipped")
	}

	tenants, err := sc.TenancyResolver().AlertingTenantsForGroups(ctx, userInfo.Groups)
	if err != nil {
		return nil, fmt.Errorf("tenancy: resolve tenants: %w", err)
	}
	if len(tenants) == 0 {
		return nil, fmt.Errorf("tenancy: none of your tenants carries alerting; access denied")
	}

	return tenancy.SelectOrgIDs(tenants, explicit)
}

// resolveAlertingOrgID is resolveAlertingOrgIDs for a tool that reads one
// tenant per call, the ruler tools: several tenants are an error that names
// them, so the caller can pass org_id.
func resolveAlertingOrgID(ctx context.Context, sc *server.ServerContext, explicit string) (string, error) {
	orgIDs, err := resolveAlertingOrgIDs(ctx, sc, explicit)
	if err != nil {
		return "", err
	}
	if len(orgIDs) > 1 {
		return "", fmt.Errorf("tenancy: this tool reads one tenant per call; pass org_id with one of your alerting tenants (%s)",
			strings.Join(orgIDs, ", "))
	}
	return orgIDs[0], nil
}

// splitOrgID splits a pipe-separated org_id into its parts, trimmed; an empty
// value is the single empty part (no tenant override).
func splitOrgID(explicit string) []string {
	parts := strings.Split(explicit, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// createClientFromParams creates a Prometheus client from request parameters,
// applying OAuth tenancy resolution for the X-Scope-OrgID header when enabled:
// the querier's federated selector for scopeData, a single alerting tenant
// for scopeAlerting.
func createClientFromParams(ctx context.Context, params map[string]any, defaultClient *Client, sc *server.ServerContext, scope tenantScope) (*Client, error) {
	prometheusURL, hasURL := params["prometheus_url"].(string)
	explicitOrgID, _ := params[paramOrgID].(string)

	var orgID string
	var err error
	if scope == scopeAlerting {
		orgID, err = resolveAlertingOrgID(ctx, sc, explicitOrgID)
	} else {
		orgID, err = resolveTenantOrgID(ctx, sc, explicitOrgID)
	}
	if err != nil {
		return nil, err
	}
	hasOrgID := orgID != ""

	// If neither parameter is provided and OAuth didn't inject an org ID, use default client
	if !hasURL && !hasOrgID {
		if defaultClient != nil && defaultClient.client != nil {
			return defaultClient, nil
		}
		return nil, fmt.Errorf("prometheus_url parameter is required (no default Prometheus configuration available)")
	}

	// Start with environment config to inherit authentication
	config := sc.PrometheusConfig()

	// Override URL if provided (validated to prevent SSRF via scheme abuse).
	if hasURL && prometheusURL != "" {
		if err := validatePrometheusURL(prometheusURL); err != nil {
			return nil, err
		}
		config.URL = prometheusURL
		sc.Logger().Debug("Overriding Prometheus URL from parameter", "url", prometheusURL)
	}

	// Set the resolved org ID (from tenancy or explicit override)
	if hasOrgID {
		config.OrgID = orgID
		sc.Logger().Debug("Setting Prometheus OrgID", "orgID", orgID)
	}

	// Validate that we have a URL
	if config.URL == "" {
		return nil, fmt.Errorf("prometheus_url parameter is required when using dynamic client configuration")
	}

	sc.Logger().Debug("Creating dynamic client with inherited config",
		"url", config.URL, "orgID", config.OrgID, "hasAuth", config.Username != "" || config.Token != "")

	// Create and return new client
	return NewClient(config, sc.Logger())
}

// handleExecuteQuery handles the execute_query tool with enhanced parameters
func handleExecuteQuery(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	params := extractParams(request)

	query, ok := params["query"].(string)
	if !ok || query == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: errQueryParameterRequired,
				},
			},
		}, nil
	}

	timeParam, _ := params["time"].(string)
	unlimited := isUnlimitedRequest(request)

	// Extract new optional parameters
	options := QueryOptions{
		Timeout:       getStringParam(params, "timeout"),
		Limit:         getStringParam(params, "limit"),
		Stats:         getStringParam(params, "stats"),
		LookbackDelta: getStringParam(params, "lookback_delta"),
	}

	sc.Logger().Debug("Executing PromQL query", "query", query, "time", timeParam, "options", options, "unlimited", unlimited)

	// Use enhanced query if any options are provided
	var result *QueryResult
	var err error
	if options.Timeout != "" || options.Limit != "" || options.Stats != "" || options.LookbackDelta != "" {
		result, err = client.ExecuteQueryWithOptions(ctx, query, timeParam, options)
	} else {
		result, err = client.ExecuteQuery(ctx, query, timeParam)
	}

	if err != nil {
		sc.Logger().Error("Failed to execute query", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: fmt.Sprintf("Error executing query: %v", err),
				},
			},
		}, nil
	}

	formattedResult := formatQueryResult(result.ResultType, result.Result, unlimited)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: contentTypeText,
				Text: formattedResult,
			},
		},
	}, nil
}

// handleExecuteRangeQuery handles the execute_range_query tool with enhanced parameters
func handleExecuteRangeQuery(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	params := extractParams(request)

	query, ok := params["query"].(string)
	if !ok || query == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: errQueryParameterRequired,
				},
			},
		}, nil
	}

	start, ok := params["start"].(string)
	if !ok || start == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: "Error: start parameter is required and must be a string",
				},
			},
		}, nil
	}

	end, ok := params["end"].(string)
	if !ok || end == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: "Error: end parameter is required and must be a string",
				},
			},
		}, nil
	}

	step, ok := params["step"].(string)
	if !ok || step == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: "Error: step parameter is required and must be a string",
				},
			},
		}, nil
	}
	unlimited := isUnlimitedRequest(request)

	// Extract new optional parameters
	options := QueryOptions{
		Timeout:       getStringParam(params, "timeout"),
		Limit:         getStringParam(params, "limit"),
		Stats:         getStringParam(params, "stats"),
		LookbackDelta: getStringParam(params, "lookback_delta"),
	}

	sc.Logger().Debug("Executing PromQL range query", "query", query, "start", start, "end", end, "step", step, "options", options, "unlimited", unlimited)

	// Use enhanced query if any options are provided
	var result *QueryResult
	var err error
	if options.Timeout != "" || options.Limit != "" || options.Stats != "" || options.LookbackDelta != "" {
		result, err = client.ExecuteRangeQueryWithOptions(ctx, query, start, end, step, options)
	} else {
		result, err = client.ExecuteRangeQuery(ctx, query, start, end, step)
	}

	if err != nil {
		sc.Logger().Error("Failed to execute range query", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: fmt.Sprintf("Error executing range query: %v", err),
				},
			},
		}, nil
	}

	formattedResult := formatQueryResult(result.ResultType, result.Result, unlimited)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: contentTypeText,
				Text: formattedResult,
			},
		},
	}, nil
}

// handleGetMetricMetadata handles the get_metric_metadata tool with enhanced options
func handleGetMetricMetadata(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	params := extractParams(request)

	metric, ok := params["metric"].(string)
	if !ok || metric == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: "Error: metric parameter is required and must be a string",
				},
			},
		}, nil
	}
	options := MetricMetadataOptions{
		Limit: getStringParam(params, "limit"),
	}

	sc.Logger().Debug("Getting metric metadata", "metric", metric, "options", options)

	metadata, err := client.GetMetricMetadataWithOptions(ctx, metric, options)
	if err != nil {
		sc.Logger().Error("Failed to get metric metadata", "error", err, "metric", metric)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: fmt.Sprintf("Error getting metadata for metric '%s': %v", metric, err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: contentTypeText,
				Text: fmt.Sprintf("Metadata for metric '%s':\n%+v", metric, metadata),
			},
		},
	}, nil
}

// handleGetTargets handles the get_targets tool (existing)
func handleGetTargets(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	sc.Logger().Debug("Getting targets")

	targets, err := client.GetTargets(ctx)
	if err != nil {
		sc.Logger().Error("Failed to get targets", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: fmt.Sprintf("Error getting targets: %v", err),
				},
			},
		}, nil
	}

	result := fmt.Sprintf("Targets information:\nActive targets: %d\nDropped targets: %d\n\nActive Targets: %+v\nDropped Targets: %+v",
		len(targets.ActiveTargets),
		len(targets.DroppedTargets),
		targets.ActiveTargets,
		targets.DroppedTargets,
	)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: contentTypeText,
				Text: result,
			},
		},
	}, nil
}

// NEW TOOL HANDLERS START HERE

// handleListLabelNames handles the list_label_names tool
func handleListLabelNames(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	params := extractParams(request)
	options := LabelOptions{
		StartTime: getStringParam(params, "start_time"),
		EndTime:   getStringParam(params, "end_time"),
		Matches:   extractStringArray(params, "matches"),
		Limit:     getStringParam(params, "limit"),
	}

	sc.Logger().Debug("Listing label names", "options", options)

	result, err := client.ListLabelNames(ctx, options)
	if err != nil {
		sc.Logger().Error("Failed to list label names", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: fmt.Sprintf("Error listing label names: %v", err),
				},
			},
		}, nil
	}

	var responseText string
	if len(result.LabelNames) == 0 {
		responseText = "No label names found"
	} else {
		responseText = fmt.Sprintf("Found %d label names:\n", len(result.LabelNames))
		for i, labelName := range result.LabelNames {
			responseText += fmt.Sprintf("%d. %s\n", i+1, labelName)
		}
	}

	if len(result.Warnings) > 0 {
		responseText += fmt.Sprintf("\nWarnings: %v", result.Warnings)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: contentTypeText,
				Text: responseText,
			},
		},
	}, nil
}

// handleListLabelValues handles the list_label_values tool
func handleListLabelValues(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	params := extractParams(request)

	label, ok := params["label"].(string)
	if !ok || label == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: "Error: label parameter is required and must be a string",
				},
			},
		}, nil
	}
	options := LabelOptions{
		StartTime: getStringParam(params, "start_time"),
		EndTime:   getStringParam(params, "end_time"),
		Matches:   extractStringArray(params, "matches"),
		Limit:     getStringParam(params, "limit"),
	}

	sc.Logger().Debug("Listing label values", "label", label, "options", options)

	result, err := client.ListLabelValues(ctx, label, options)
	if err != nil {
		sc.Logger().Error("Failed to list label values", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: fmt.Sprintf("Error listing label values for '%s': %v", label, err),
				},
			},
		}, nil
	}

	var responseText string
	if len(result.LabelValues) == 0 {
		responseText = fmt.Sprintf("No values found for label '%s'", label)
	} else {
		responseText = fmt.Sprintf("Found %d values for label '%s':\n", len(result.LabelValues), label)
		for i, value := range result.LabelValues {
			responseText += fmt.Sprintf("%d. %s\n", i+1, value)
			// Limit output for very long lists
			if i >= 99 {
				responseText += fmt.Sprintf("... and %d more values\n", len(result.LabelValues)-100)
				break
			}
		}
	}

	if len(result.Warnings) > 0 {
		responseText += fmt.Sprintf("\nWarnings: %v", result.Warnings)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: contentTypeText,
				Text: responseText,
			},
		},
	}, nil
}

// handleFindSeries handles the find_series tool
func handleFindSeries(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	params := extractParams(request)

	matches := extractStringArray(params, "matches")
	if len(matches) == 0 {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: "Error: matches parameter is required and must be an array of strings",
				},
			},
		}, nil
	}
	options := SeriesOptions{
		StartTime: getStringParam(params, "start_time"),
		EndTime:   getStringParam(params, "end_time"),
		Limit:     getStringParam(params, "limit"),
	}

	sc.Logger().Debug("Finding series", "matches", matches, "options", options)

	result, err := client.FindSeries(ctx, matches, options)
	if err != nil {
		sc.Logger().Error("Failed to find series", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: fmt.Sprintf("Error finding series: %v", err),
				},
			},
		}, nil
	}

	var responseText string
	if len(result.Series) == 0 {
		responseText = "No series found matching the given criteria"
	} else {
		responseText = fmt.Sprintf("Found %d series:\n", len(result.Series))
		for i, series := range result.Series {
			responseText += fmt.Sprintf("%d. %+v\n", i+1, series)
			// Limit output for very long lists
			if i >= 49 {
				responseText += fmt.Sprintf("... and %d more series\n", len(result.Series)-50)
				break
			}
		}
	}

	if len(result.Warnings) > 0 {
		responseText += fmt.Sprintf("\nWarnings: %v", result.Warnings)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: contentTypeText,
				Text: responseText,
			},
		},
	}, nil
}

// handleGetRules handles the get_rules tool
func handleGetRules(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	params := extractParams(request)

	options := RulesOptions{
		Type:          getStringParam(params, paramRuleType),
		RuleNames:     extractStringArray(params, paramRuleName),
		RuleGroups:    extractStringArray(params, paramRuleGroup),
		Files:         extractStringArray(params, paramRuleFile),
		ExcludeAlerts: getBoolParam(params, paramExcludeAlerts),
	}
	sc.Logger().Debug("Getting rules", "options", options)

	rules, err := client.GetRules(ctx, options)
	if err != nil {
		sc.Logger().Error("Failed to get rules", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: fmt.Sprintf("Error getting rules: %v", err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: contentTypeText,
				Text: fmt.Sprintf("Prometheus Rules:\n%+v", rules),
			},
		},
	}, nil
}

// handleGetAlerts handles the get_alerts tool
func handleGetAlerts(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	sc.Logger().Debug("Getting alerts")

	alerts, err := client.GetAlerts(ctx)
	if err != nil {
		sc.Logger().Error("Failed to get alerts", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: fmt.Sprintf("Error getting alerts: %v", err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: contentTypeText,
				Text: fmt.Sprintf("Active Alerts:\n%+v", alerts),
			},
		},
	}, nil
}

// handleGetAlertManagers handles the get_alertmanagers tool
func handleGetAlertManagers(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	sc.Logger().Debug("Getting alert managers")

	alertManagers, err := client.GetAlertManagers(ctx)
	if err != nil {
		sc.Logger().Error("Failed to get alert managers", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: fmt.Sprintf("Error getting alert managers: %v", err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: contentTypeText,
				Text: fmt.Sprintf("AlertManager Discovery:\n%+v", alertManagers),
			},
		},
	}, nil
}

// handleGetConfig handles the get_config tool
func handleGetConfig(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	sc.Logger().Debug("Getting config")

	config, err := client.GetConfig(ctx)
	if err != nil {
		sc.Logger().Error("Failed to get config", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: fmt.Sprintf("Error getting config: %v", err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: contentTypeText,
				Text: fmt.Sprintf("Prometheus Configuration:\n%+v", config),
			},
		},
	}, nil
}

// handleGetFlags handles the get_flags tool
func handleGetFlags(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	sc.Logger().Debug("Getting flags")

	flags, err := client.GetFlags(ctx)
	if err != nil {
		sc.Logger().Error("Failed to get flags", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: fmt.Sprintf("Error getting flags: %v", err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: contentTypeText,
				Text: fmt.Sprintf("Prometheus Runtime Flags:\n%+v", flags),
			},
		},
	}, nil
}

// handleGetBuildInfo handles the get_build_info tool
func handleGetBuildInfo(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	sc.Logger().Debug("Getting build info")

	buildInfo, err := client.GetBuildInfo(ctx)
	if err != nil {
		sc.Logger().Error("Failed to get build info", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: fmt.Sprintf("Error getting build info: %v", err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: contentTypeText,
				Text: fmt.Sprintf("Prometheus Build Information:\n%+v", buildInfo),
			},
		},
	}, nil
}

// handleGetRuntimeInfo handles the get_runtime_info tool
func handleGetRuntimeInfo(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	sc.Logger().Debug("Getting runtime info")

	runtimeInfo, err := client.GetRuntimeInfo(ctx)
	if err != nil {
		sc.Logger().Error("Failed to get runtime info", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: fmt.Sprintf("Error getting runtime info: %v", err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: contentTypeText,
				Text: fmt.Sprintf("Prometheus Runtime Information:\n%+v", runtimeInfo),
			},
		},
	}, nil
}

// handleGetTSDBStats handles the get_tsdb_stats tool
func handleGetTSDBStats(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	params := extractParams(request)

	options := TSDBOptions{
		Limit: getStringParam(params, "limit"),
	}
	sc.Logger().Debug("Getting TSDB stats", "options", options)

	tsdbStats, err := client.GetTSDBStats(ctx, options)
	if err != nil {
		sc.Logger().Error("Failed to get TSDB stats", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: fmt.Sprintf("Error getting TSDB stats: %v", err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: contentTypeText,
				Text: fmt.Sprintf("TSDB Statistics:\n%+v", tsdbStats),
			},
		},
	}, nil
}

// handleQueryExemplars handles the query_exemplars tool
func handleQueryExemplars(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	params := extractParams(request)

	query, ok := params["query"].(string)
	if !ok || query == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: errQueryParameterRequired,
				},
			},
		}, nil
	}

	start, ok := params["start"].(string)
	if !ok || start == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: "Error: start parameter is required and must be a string",
				},
			},
		}, nil
	}

	end, ok := params["end"].(string)
	if !ok || end == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: "Error: end parameter is required and must be a string",
				},
			},
		}, nil
	}
	sc.Logger().Debug("Querying exemplars", "query", query, "start", start, "end", end)

	exemplars, err := client.QueryExemplars(ctx, query, start, end)
	if err != nil {
		sc.Logger().Error("Failed to query exemplars", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: fmt.Sprintf("Error querying exemplars: %v", err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: contentTypeText,
				Text: fmt.Sprintf("Exemplars for query '%s':\n%+v", query, exemplars),
			},
		},
	}, nil
}

// handleGetTargetsMetadata handles the get_targets_metadata tool
func handleGetTargetsMetadata(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	params := extractParams(request)

	matchTarget := getStringParam(params, "match_target")
	metric := getStringParam(params, "metric")
	limit := getStringParam(params, "limit")
	sc.Logger().Debug("Getting targets metadata", "match_target", matchTarget, "metric", metric, "limit", limit)

	targetsMetadata, err := client.GetTargetsMetadata(ctx, matchTarget, metric, limit)
	if err != nil {
		sc.Logger().Error("Failed to get targets metadata", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: contentTypeText,
					Text: fmt.Sprintf("Error getting targets metadata: %v", err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: contentTypeText,
				Text: fmt.Sprintf("Targets Metadata:\n%+v", targetsMetadata),
			},
		},
	}, nil
}

// handleCheckReady handles the check_ready tool
func handleCheckReady(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	sc.Logger().Debug("Checking Prometheus readiness")

	status, err := client.CheckReady(ctx)
	if err != nil {
		sc.Logger().Error("Failed to check readiness", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{Type: contentTypeText, Text: fmt.Sprintf("Error checking readiness: %v", err)},
			},
		}, nil
	}

	if !status.Ready {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{Type: contentTypeText, Text: fmt.Sprintf("Prometheus is not ready (HTTP %d): %s", status.StatusCode, status.Message)},
			},
		}, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{Type: contentTypeText, Text: fmt.Sprintf("Prometheus is ready (HTTP %d): %s", status.StatusCode, status.Message)},
		},
	}, nil
}

// Helper function to safely get string parameter
func getStringParam(params map[string]any, key string) string {
	if val, ok := params[key].(string); ok {
		return val
	}
	return ""
}

// getBoolParam returns the boolean value of key, or false when absent or not
// a JSON boolean (the input schema rejects other types before the handler runs).
func getBoolParam(params map[string]any, key string) bool {
	val, _ := params[key].(bool)
	return val
}
