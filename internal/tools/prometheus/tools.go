package prometheus

import (
	"context"
	"fmt"
	"strings"

	"github.com/giantswarm/mcp-prometheus/internal/server"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
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
)

// Common parameter builders to reduce repetition
func withPrometheusConnectionParams(options ...mcp.ToolOption) []mcp.ToolOption {
	connectionParams := []mcp.ToolOption{
		mcp.WithString("prometheus_url",
			mcp.Description("Prometheus server URL (e.g., 'http://localhost:8080/prometheus')"),
		),
		mcp.WithString("org_id",
			mcp.Description("Organization ID for multi-tenant Prometheus"),
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
			mcp.Description("Maximum number of returned entries"),
		),
		mcp.WithString("stats",
			mcp.Description("Include query statistics: 'all'"),
		),
		mcp.WithString("lookback_delta",
			mcp.Description("Query lookback delta (e.g., '5m')"),
		),
		mcp.WithString("unlimited",
			mcp.Description("Set to 'true' to get unlimited output (WARNING: may be very large and impact performance)"),
		),
	}
	return append(enhancementParams, options...)
}

func withTimeFilteringParams(options ...mcp.ToolOption) []mcp.ToolOption {
	timeParams := []mcp.ToolOption{
		mcp.WithString("start_time",
			mcp.Description("Start time for filtering (RFC3339)"),
		),
		mcp.WithString("end_time",
			mcp.Description("End time for filtering (RFC3339)"),
		),
	}
	return append(timeParams, options...)
}

func withLabelMatchingParams(options ...mcp.ToolOption) []mcp.ToolOption {
	matchParams := []mcp.ToolOption{
		mcp.WithArray("matches",
			mcp.Description("Array of label matchers to filter series"),
		),
	}
	return append(matchParams, options...)
}

// Handler wrapper type for cleaner function signatures
type PrometheusHandler func(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error)

// Wrapper that handles dynamic client creation and error handling
func withDynamicPrometheusClient(handler PrometheusHandler, client *Client, sc *server.ServerContext) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := extractParams(request)

		// Create client with dynamic parameters if provided
		dynamicClient, err := createClientFromParams(params, client, sc)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: fmt.Sprintf("Error creating Prometheus client: %v", err),
					},
				},
			}, nil
		}

		// Call the actual handler with the dynamic client
		return handler(ctx, request, dynamicClient, sc)
	}
}

// Helper function to create and register a tool with common patterns
func registerPrometheusTools(s *mcpserver.MCPServer, client *Client, sc *server.ServerContext, toolName string, description string, handler PrometheusHandler, options ...mcp.ToolOption) {
	// Always include prometheus connection parameters
	allOptions := withPrometheusConnectionParams(options...)
	
	tool := mcp.NewTool(toolName, append([]mcp.ToolOption{mcp.WithDescription(description)}, allOptions...)...)
	s.AddTool(tool, withDynamicPrometheusClient(handler, client, sc))
}

// RegisterPrometheusTools registers Prometheus-related tools with the MCP server
func RegisterPrometheusTools(s *mcpserver.MCPServer, sc *server.ServerContext) error {
	// Create Prometheus client
	client := NewClient(sc.PrometheusConfig(), sc.Logger())

	// Query execution tools
	registerPrometheusTools(s, client, sc, "execute_query", "Execute a PromQL instant query against Prometheus",
		handleExecuteQuery, withQueryEnhancementParams(
			mcp.WithString("query", mcp.Required(), mcp.Description("PromQL query string")),
			mcp.WithString("time", mcp.Description("Optional RFC3339 or Unix timestamp (default: current time)")),
		)...)

	registerPrometheusTools(s, client, sc, "execute_range_query", "Execute a PromQL range query with start time, end time, and step interval",
		handleExecuteRangeQuery, withQueryEnhancementParams(
			mcp.WithString("query", mcp.Required(), mcp.Description("PromQL query string")),
			mcp.WithString("start", mcp.Required(), mcp.Description("Start time as RFC3339 or Unix timestamp")),
			mcp.WithString("end", mcp.Required(), mcp.Description("End time as RFC3339 or Unix timestamp")),
			mcp.WithString("step", mcp.Required(), mcp.Description("Query resolution step width (e.g., '15s', '1m', '1h')")),
		)...)

	// Metrics discovery tools
	registerPrometheusTools(s, client, sc, "list_metrics", "List all available metrics in Prometheus",
		handleListMetrics, withTimeFilteringParams(withLabelMatchingParams()...)...)

	registerPrometheusTools(s, client, sc, "get_metric_metadata", "Get metadata for a specific metric",
		handleGetMetricMetadata,
		mcp.WithString("metric", mcp.Required(), mcp.Description("The name of the metric to retrieve metadata for")),
		mcp.WithString("limit", mcp.Description("Maximum number of metadata entries to return")),
	)

	// Label and series discovery tools
	registerPrometheusTools(s, client, sc, "list_label_names", "Get all available label names",
		handleListLabelNames, withTimeFilteringParams(withLabelMatchingParams(
			mcp.WithString("limit", mcp.Description("Maximum number of label names to return")),
		)...)...)

	registerPrometheusTools(s, client, sc, "list_label_values", "Get values for a specific label",
		handleListLabelValues, withTimeFilteringParams(withLabelMatchingParams(
			mcp.WithString("label", mcp.Required(), mcp.Description("The label name to get values for")),
			mcp.WithString("limit", mcp.Description("Maximum number of label values to return")),
		)...)...)

	registerPrometheusTools(s, client, sc, "find_series", "Find series by label matchers",
		handleFindSeries, withTimeFilteringParams(
			mcp.WithArray("matches", mcp.Required(), mcp.Description("Array of label matchers (e.g., ['{job=\"prometheus\"}', '{__name__=~\"http_.*\"}'])")),
			mcp.WithString("limit", mcp.Description("Maximum number of series to return")),
		)...)

	// Target and system information tools
	registerPrometheusTools(s, client, sc, "get_targets", "Get information about all scrape targets", handleGetTargets)

	registerPrometheusTools(s, client, sc, "get_build_info", "Get build information about the Prometheus server", handleGetBuildInfo)

	registerPrometheusTools(s, client, sc, "get_runtime_info", "Get runtime information about the Prometheus server", handleGetRuntimeInfo)

	registerPrometheusTools(s, client, sc, "get_flags", "Get runtime flags that Prometheus was launched with", handleGetFlags)

	registerPrometheusTools(s, client, sc, "get_config", "Get Prometheus configuration", handleGetConfig)

	// Alerting tools
	registerPrometheusTools(s, client, sc, "get_alerts", "Get active alerts", handleGetAlerts)

	registerPrometheusTools(s, client, sc, "get_alertmanagers", "Get AlertManager discovery information", handleGetAlertManagers)

	registerPrometheusTools(s, client, sc, "get_rules", "Get recording and alerting rules", handleGetRules)

	// Advanced tools
	registerPrometheusTools(s, client, sc, "get_tsdb_stats", "Get TSDB cardinality statistics",
		handleGetTSDBStats,
		mcp.WithString("limit", mcp.Description("Maximum number of stats entries to return")),
	)

	registerPrometheusTools(s, client, sc, "query_exemplars", "Query exemplars for traces",
		handleQueryExemplars,
		mcp.WithString("query", mcp.Required(), mcp.Description("PromQL query string to find exemplars for")),
		mcp.WithString("start", mcp.Required(), mcp.Description("Start time as RFC3339 or Unix timestamp")),
		mcp.WithString("end", mcp.Required(), mcp.Description("End time as RFC3339 or Unix timestamp")),
	)

	registerPrometheusTools(s, client, sc, "get_targets_metadata", "Get metadata about metrics from specific targets",
		handleGetTargetsMetadata,
		mcp.WithString("match_target", mcp.Description("Target matcher to filter targets")),
		mcp.WithString("metric", mcp.Description("Metric name to filter metadata for")),
		mcp.WithString("limit", mcp.Description("Maximum number of metadata entries to return")),
	)

	return nil
}

// formatQueryResult formats the query result with truncation and user guidance
func formatQueryResult(resultType string, result interface{}, unlimited bool) string {
	resultStr := fmt.Sprintf("Query executed successfully.\nResult Type: %s\nResult: %+v", resultType, result)

	if unlimited {
		warningMsg := "⚠️  WARNING: Unlimited output enabled - this response may be very large and could impact performance.\n\n"
		return warningMsg + resultStr
	}

	if len(resultStr) > MaxResultLength {
		truncated := resultStr[:MaxResultLength]
		// Try to end at a complete line to avoid cutting off mid-metric
		if lastNewline := strings.LastIndex(truncated, "\n"); lastNewline > MaxResultLength-1000 {
			truncated = truncated[:lastNewline]
		}
		return truncated + TruncationAdvice
	}

	return resultStr
}

// Helper function to extract parameters
func extractParams(request mcp.CallToolRequest) map[string]interface{} {
	params := make(map[string]interface{})
	if request.Params.Arguments != nil {
		if argsMap, ok := request.Params.Arguments.(map[string]interface{}); ok {
			params = argsMap
		}
	}
	return params
}

// Helper function to extract string array parameter
func extractStringArray(params map[string]interface{}, key string) []string {
	if val, ok := params[key]; ok {
		if arr, ok := val.([]interface{}); ok {
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

// Helper function to create a client with dynamic parameters
func createClientFromParams(params map[string]interface{}, defaultClient *Client, sc *server.ServerContext) (*Client, error) {
	prometheusURL, hasURL := params["prometheus_url"].(string)
	orgID, hasOrgID := params["org_id"].(string)

	// If neither parameter is provided, check if default client is valid
	if !hasURL && !hasOrgID {
		if defaultClient != nil && defaultClient.client != nil {
			return defaultClient, nil
		}
		// If default client is not available, require dynamic parameters
		return nil, fmt.Errorf("prometheus_url parameter is required (no default Prometheus configuration available)")
	}

	// Always create a new client when dynamic parameters are provided
	// Start with empty config to avoid inheriting potentially empty defaults
	config := server.PrometheusConfig{}
	
	// Set URL if provided
	if hasURL && prometheusURL != "" {
		config.URL = prometheusURL
		sc.Logger().Debug("Setting Prometheus URL from parameter", "url", prometheusURL)
	}
	
	// Set OrgID if provided
	if hasOrgID && orgID != "" {
		config.OrgID = orgID
		sc.Logger().Debug("Setting Prometheus OrgID from parameter", "orgID", orgID)
	}

	// Validate that we have a URL when creating a new client
	if config.URL == "" {
		return nil, fmt.Errorf("prometheus_url parameter is required when using dynamic client configuration")
	}

	sc.Logger().Debug("Creating dynamic client with config", "url", config.URL, "orgID", config.OrgID)

	// Create and return new client
	client := NewClient(config, sc.Logger())
	if client.client == nil {
		return nil, fmt.Errorf("failed to initialize Prometheus client with URL: %s, OrgID: %s", config.URL, config.OrgID)
	}

	return client, nil
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
					Type: "text",
					Text: "Error: query parameter is required and must be a string",
				},
			},
		}, nil
	}

	timeParam, _ := params["time"].(string)
	unlimitedStr, _ := params["unlimited"].(string)
	unlimited := unlimitedStr == "true"

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
		result, err = client.ExecuteQueryWithOptions(query, timeParam, options)
	} else {
		result, err = client.ExecuteQuery(query, timeParam)
	}

	if err != nil {
		sc.Logger().Error("Failed to execute query", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error executing query: %v", err),
				},
			},
		}, nil
	}

	formattedResult := formatQueryResult(result.ResultType, result.Result, unlimited)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
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
					Type: "text",
					Text: "Error: query parameter is required and must be a string",
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
					Type: "text",
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
					Type: "text",
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
					Type: "text",
					Text: "Error: step parameter is required and must be a string",
				},
			},
		}, nil
	}
	unlimitedStr, _ := params["unlimited"].(string)
	unlimited := unlimitedStr == "true"

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
		result, err = client.ExecuteRangeQueryWithOptions(query, start, end, step, options)
	} else {
		result, err = client.ExecuteRangeQuery(query, start, end, step)
	}

	if err != nil {
		sc.Logger().Error("Failed to execute range query", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error executing range query: %v", err),
				},
			},
		}, nil
	}

	formattedResult := formatQueryResult(result.ResultType, result.Result, unlimited)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: formattedResult,
			},
		},
	}, nil
}

// handleListMetrics handles the list_metrics tool with enhanced filtering
func handleListMetrics(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	params := extractParams(request)
	options := ListMetricsOptions{
		StartTime: getStringParam(params, "start_time"),
		EndTime:   getStringParam(params, "end_time"),
		Matches:   extractStringArray(params, "matches"),
	}

	sc.Logger().Debug("Listing metrics", "options", options)

	metrics, err := client.ListMetricsWithOptions(options)
	if err != nil {
		sc.Logger().Error("Failed to list metrics", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error listing metrics: %v", err),
				},
			},
		}, nil
	}

	var result string
	if len(metrics) == 0 {
		result = "No metrics found"
	} else {
		result = fmt.Sprintf("Found %d metrics:\n", len(metrics))
		for i, metric := range metrics {
			result += fmt.Sprintf("%d. %s\n", i+1, metric)
			// Limit output to prevent overwhelming the response
			if i >= 99 {
				result += fmt.Sprintf("... and %d more metrics\n", len(metrics)-100)
				break
			}
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: result,
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
					Type: "text",
					Text: "Error: metric parameter is required and must be a string",
				},
			},
		}, nil
	}
	options := MetricMetadataOptions{
		Limit: getStringParam(params, "limit"),
	}

	sc.Logger().Debug("Getting metric metadata", "metric", metric, "options", options)

	metadata, err := client.GetMetricMetadataWithOptions(metric, options)
	if err != nil {
		sc.Logger().Error("Failed to get metric metadata", "error", err, "metric", metric)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error getting metadata for metric '%s': %v", metric, err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Metadata for metric '%s':\n%+v", metric, metadata),
			},
		},
	}, nil
}

// handleGetTargets handles the get_targets tool (existing)
func handleGetTargets(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	sc.Logger().Debug("Getting targets")

	targets, err := client.GetTargets()
	if err != nil {
		sc.Logger().Error("Failed to get targets", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
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
				Type: "text",
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

	result, err := client.ListLabelNames(options)
	if err != nil {
		sc.Logger().Error("Failed to list label names", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
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
				Type: "text",
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
					Type: "text",
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

	result, err := client.ListLabelValues(label, options)
	if err != nil {
		sc.Logger().Error("Failed to list label values", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
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
				Type: "text",
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
					Type: "text",
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

	result, err := client.FindSeries(matches, options)
	if err != nil {
		sc.Logger().Error("Failed to find series", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
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
				Type: "text",
				Text: responseText,
			},
		},
	}, nil
}

// handleGetRules handles the get_rules tool
func handleGetRules(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	sc.Logger().Debug("Getting rules")

	rules, err := client.GetRules()
	if err != nil {
		sc.Logger().Error("Failed to get rules", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error getting rules: %v", err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Prometheus Rules:\n%+v", rules),
			},
		},
	}, nil
}

// handleGetAlerts handles the get_alerts tool
func handleGetAlerts(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	sc.Logger().Debug("Getting alerts")

	alerts, err := client.GetAlerts()
	if err != nil {
		sc.Logger().Error("Failed to get alerts", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error getting alerts: %v", err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Active Alerts:\n%+v", alerts),
			},
		},
	}, nil
}

// handleGetAlertManagers handles the get_alertmanagers tool
func handleGetAlertManagers(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	sc.Logger().Debug("Getting alert managers")

	alertManagers, err := client.GetAlertManagers()
	if err != nil {
		sc.Logger().Error("Failed to get alert managers", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error getting alert managers: %v", err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("AlertManager Discovery:\n%+v", alertManagers),
			},
		},
	}, nil
}

// handleGetConfig handles the get_config tool
func handleGetConfig(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	sc.Logger().Debug("Getting config")

	config, err := client.GetConfig()
	if err != nil {
		sc.Logger().Error("Failed to get config", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error getting config: %v", err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Prometheus Configuration:\n%+v", config),
			},
		},
	}, nil
}

// handleGetFlags handles the get_flags tool
func handleGetFlags(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	sc.Logger().Debug("Getting flags")

	flags, err := client.GetFlags()
	if err != nil {
		sc.Logger().Error("Failed to get flags", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error getting flags: %v", err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Prometheus Runtime Flags:\n%+v", flags),
			},
		},
	}, nil
}

// handleGetBuildInfo handles the get_build_info tool
func handleGetBuildInfo(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	sc.Logger().Debug("Getting build info")

	buildInfo, err := client.GetBuildInfo()
	if err != nil {
		sc.Logger().Error("Failed to get build info", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error getting build info: %v", err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Prometheus Build Information:\n%+v", buildInfo),
			},
		},
	}, nil
}

// handleGetRuntimeInfo handles the get_runtime_info tool
func handleGetRuntimeInfo(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	sc.Logger().Debug("Getting runtime info")

	runtimeInfo, err := client.GetRuntimeInfo()
	if err != nil {
		sc.Logger().Error("Failed to get runtime info", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error getting runtime info: %v", err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
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

	tsdbStats, err := client.GetTSDBStats(options)
	if err != nil {
		sc.Logger().Error("Failed to get TSDB stats", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error getting TSDB stats: %v", err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
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
					Type: "text",
					Text: "Error: query parameter is required and must be a string",
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
					Type: "text",
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
					Type: "text",
					Text: "Error: end parameter is required and must be a string",
				},
			},
		}, nil
	}
	sc.Logger().Debug("Querying exemplars", "query", query, "start", start, "end", end)

	exemplars, err := client.QueryExemplars(query, start, end)
	if err != nil {
		sc.Logger().Error("Failed to query exemplars", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error querying exemplars: %v", err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
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

	targetsMetadata, err := client.GetTargetsMetadata(matchTarget, metric, limit)
	if err != nil {
		sc.Logger().Error("Failed to get targets metadata", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Error getting targets metadata: %v", err),
				},
			},
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Targets Metadata:\n%+v", targetsMetadata),
			},
		},
	}, nil
}

// Helper function to safely get string parameter
func getStringParam(params map[string]interface{}, key string) string {
	if val, ok := params[key].(string); ok {
		return val
	}
	return ""
}
