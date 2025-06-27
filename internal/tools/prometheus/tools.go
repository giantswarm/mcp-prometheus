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

// RegisterPrometheusTools registers Prometheus-related tools with the MCP server
func RegisterPrometheusTools(s *mcpserver.MCPServer, sc *server.ServerContext) error {
	// Create Prometheus client
	client := NewClient(sc.PrometheusConfig(), sc.Logger())

	// execute_query tool
	executeQueryTool := mcp.NewTool("execute_query",
		mcp.WithDescription("Execute a PromQL instant query against Prometheus"),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("PromQL query string"),
		),
		mcp.WithString("time",
			mcp.Description("Optional RFC3339 or Unix timestamp (default: current time)"),
		),
		mcp.WithString("unlimited",
			mcp.Description("Set to 'true' to get unlimited output (WARNING: may be very large and impact performance)"),
		),
	)

	s.AddTool(executeQueryTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleExecuteQuery(ctx, request, client, sc)
	})

	// execute_range_query tool
	executeRangeQueryTool := mcp.NewTool("execute_range_query",
		mcp.WithDescription("Execute a PromQL range query with start time, end time, and step interval"),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("PromQL query string"),
		),
		mcp.WithString("start",
			mcp.Required(),
			mcp.Description("Start time as RFC3339 or Unix timestamp"),
		),
		mcp.WithString("end",
			mcp.Required(),
			mcp.Description("End time as RFC3339 or Unix timestamp"),
		),
		mcp.WithString("step",
			mcp.Required(),
			mcp.Description("Query resolution step width (e.g., '15s', '1m', '1h')"),
		),
		mcp.WithString("unlimited",
			mcp.Description("Set to 'true' to get unlimited output (WARNING: may be very large and impact performance)"),
		),
	)

	s.AddTool(executeRangeQueryTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleExecuteRangeQuery(ctx, request, client, sc)
	})

	// list_metrics tool
	listMetricsTool := mcp.NewTool("list_metrics",
		mcp.WithDescription("List all available metrics in Prometheus"),
	)

	s.AddTool(listMetricsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleListMetrics(ctx, request, client, sc)
	})

	// get_metric_metadata tool
	getMetricMetadataTool := mcp.NewTool("get_metric_metadata",
		mcp.WithDescription("Get metadata for a specific metric"),
		mcp.WithString("metric",
			mcp.Required(),
			mcp.Description("The name of the metric to retrieve metadata for"),
		),
	)

	s.AddTool(getMetricMetadataTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleGetMetricMetadata(ctx, request, client, sc)
	})

	// get_targets tool
	getTargetsTool := mcp.NewTool("get_targets",
		mcp.WithDescription("Get information about all scrape targets"),
	)

	s.AddTool(getTargetsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleGetTargets(ctx, request, client, sc)
	})

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

// handleExecuteQuery handles the execute_query tool
func handleExecuteQuery(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	// Extract parameters
	params := make(map[string]interface{})
	if request.Params.Arguments != nil {
		if argsMap, ok := request.Params.Arguments.(map[string]interface{}); ok {
			params = argsMap
		}
	}

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

	sc.Logger().Debug("Executing PromQL query", "query", query, "time", timeParam, "unlimited", unlimited)

	result, err := client.ExecuteQuery(query, timeParam)
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

// handleExecuteRangeQuery handles the execute_range_query tool
func handleExecuteRangeQuery(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	// Extract parameters
	params := make(map[string]interface{})
	if request.Params.Arguments != nil {
		if argsMap, ok := request.Params.Arguments.(map[string]interface{}); ok {
			params = argsMap
		}
	}

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

	sc.Logger().Debug("Executing PromQL range query", "query", query, "start", start, "end", end, "step", step, "unlimited", unlimited)

	result, err := client.ExecuteRangeQuery(query, start, end, step)
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

// handleListMetrics handles the list_metrics tool
func handleListMetrics(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	sc.Logger().Debug("Listing metrics")

	metrics, err := client.ListMetrics()
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

// handleGetMetricMetadata handles the get_metric_metadata tool
func handleGetMetricMetadata(ctx context.Context, request mcp.CallToolRequest, client *Client, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	// Extract parameters
	params := make(map[string]interface{})
	if request.Params.Arguments != nil {
		if argsMap, ok := request.Params.Arguments.(map[string]interface{}); ok {
			params = argsMap
		}
	}

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

	sc.Logger().Debug("Getting metric metadata", "metric", metric)

	metadata, err := client.GetMetricMetadata(metric)
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

// handleGetTargets handles the get_targets tool
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
