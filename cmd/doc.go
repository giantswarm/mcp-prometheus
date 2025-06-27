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
//   - PROMETHEUS_URL: Required Prometheus server URL
//   - PROMETHEUS_USERNAME: Optional basic auth username
//   - PROMETHEUS_PASSWORD: Optional basic auth password
//   - PROMETHEUS_TOKEN: Optional bearer token for authentication
//   - ORG_ID: Optional organization ID for multi-tenant setups
//
// Example usage:
//
//	mcp-prometheus serve --transport stdio
//	mcp-prometheus serve --transport sse --http-addr :8080
package cmd
