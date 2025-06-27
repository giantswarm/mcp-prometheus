// Package server provides the core server infrastructure for the MCP Prometheus server.
//
// This package contains:
// - ServerContext: Configuration and shared resources management
// - Logger interface: Structured logging abstraction
// - Configuration options: Functional options pattern for server setup
//
// The ServerContext manages the lifecycle of the server and provides
// thread-safe access to configuration options such as:
// - Debug mode toggle
// - Dry run simulation mode
// - Prometheus connection settings
// - Authentication credentials
//
// Example usage:
//
//	ctx := context.Background()
//	serverContext, err := server.NewServerContext(ctx,
//	    server.WithDebugMode(true),
//	    server.WithLogger(logger),
//	)
package server