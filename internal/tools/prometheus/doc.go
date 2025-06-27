// Package prometheus provides MCP tools for interacting with Prometheus servers.
//
// This package implements the following MCP tools:
//
// Query Tools:
//   - execute_query: Execute PromQL instant queries
//   - execute_range_query: Execute PromQL range queries with time bounds
//
// Discovery Tools:
//   - list_metrics: List all available metrics
//   - get_metric_metadata: Get metadata for specific metrics
//   - get_targets: Get information about scrape targets
//
// Authentication Support:
//   - Basic authentication via username/password
//   - Bearer token authentication
//   - Multi-tenant organization ID headers
//
// All tools support the standard Prometheus HTTP API and handle
// authentication automatically based on the server configuration.
//
// Example tool usage:
//
//	execute_query: {"query": "up", "time": "2023-01-01T00:00:00Z"}
//	execute_range_query: {"query": "rate(http_requests_total[5m])", "start": "2023-01-01T00:00:00Z", "end": "2023-01-01T01:00:00Z", "step": "1m"}
//	list_metrics: {}
//	get_metric_metadata: {"metric": "http_requests_total"}
//	get_targets: {}
package prometheus