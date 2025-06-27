// Package cmd provides the command-line interface for the MCP Prometheus server.
//
// This package implements the Cobra CLI framework to provide commands for:
// - Starting the MCP server with various transport options (stdio, sse, http)
// - Managing server configuration and lifecycle
// - Self-updating capabilities
//
// The main entry point is the serve command which starts the MCP server and
// registers all Prometheus-related tools for querying metrics, discovering
// metrics metadata, and retrieving target information.
//
// Environment Variables:
//   - PROMETHEUS_URL: Optional Prometheus server URL (takes precedence over tool parameters)
//   - PROMETHEUS_ORGID: Optional organization ID for multi-tenant setups (takes precedence over tool parameters)
//   - PROMETHEUS_USERNAME: Optional basic auth username
//   - PROMETHEUS_PASSWORD: Optional basic auth password
//   - PROMETHEUS_TOKEN: Optional bearer token for authentication
//
// If PROMETHEUS_URL or PROMETHEUS_ORGID environment variables are not set,
// they can be provided as parameters to individual tool calls.
//
// Example usage:
//
//	mcp-prometheus serve --transport stdio
//	mcp-prometheus serve --transport sse --http-addr :8080
package cmd
