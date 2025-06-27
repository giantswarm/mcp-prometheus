package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/giantswarm/mcp-prometheus/internal/server"
	"github.com/giantswarm/mcp-prometheus/internal/tools/prometheus"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// simpleLogger provides basic logging for the server
type simpleLogger struct{}

func (l *simpleLogger) Debug(msg string, args ...interface{}) {
	log.Printf("[DEBUG] %s %v", msg, args)
}

func (l *simpleLogger) Info(msg string, args ...interface{}) {
	log.Printf("[INFO] %s %v", msg, args)
}

func (l *simpleLogger) Warn(msg string, args ...interface{}) {
	log.Printf("[WARN] %s %v", msg, args)
}

func (l *simpleLogger) Error(msg string, args ...interface{}) {
	log.Printf("[ERROR] %s %v", msg, args)
}

// newServeCmd creates the Cobra command for starting the MCP server.
func newServeCmd() *cobra.Command {
	var (
		debugMode bool

		// Transport options
		transport       string
		httpAddr        string
		sseEndpoint     string
		messageEndpoint string
		httpEndpoint    string
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP Prometheus server",
		Long: `Start the MCP Prometheus server to provide tools for interacting
with Prometheus metrics via the Model Context Protocol.

Supports multiple transport types:
  - stdio: Standard input/output (default)
  - sse: Server-Sent Events over HTTP
  - streamable-http: Streamable HTTP transport

Environment Variables:
  PROMETHEUS_URL      - Optional: Prometheus server URL (takes precedence over tool parameters)
  PROMETHEUS_ORGID    - Optional: Organization ID for multi-tenant setups (takes precedence over tool parameters)
  PROMETHEUS_USERNAME - Optional: Basic auth username
  PROMETHEUS_PASSWORD - Optional: Basic auth password
  PROMETHEUS_TOKEN    - Optional: Bearer token for authentication

If PROMETHEUS_URL or PROMETHEUS_ORGID environment variables are not set,
they can be provided as parameters to individual tool calls.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(transport, debugMode,
				httpAddr, sseEndpoint, messageEndpoint, httpEndpoint)
		},
	}

	// Add flags for configuring the server
	cmd.Flags().BoolVar(&debugMode, "debug", false, "Enable debug logging (default: false)")

	// Transport flags
	cmd.Flags().StringVar(&transport, "transport", "stdio", "Transport type: stdio, sse, or streamable-http")
	cmd.Flags().StringVar(&httpAddr, "http-addr", ":8080", "HTTP server address (for sse and streamable-http transports)")
	cmd.Flags().StringVar(&sseEndpoint, "sse-endpoint", "/sse", "SSE endpoint path (for sse transport)")
	cmd.Flags().StringVar(&messageEndpoint, "message-endpoint", "/message", "Message endpoint path (for sse transport)")
	cmd.Flags().StringVar(&httpEndpoint, "http-endpoint", "/mcp", "HTTP endpoint path (for streamable-http transport)")

	return cmd
}

// runServe contains the main server logic with support for multiple transports
func runServe(transport string, debugMode bool,
	httpAddr, sseEndpoint, messageEndpoint, httpEndpoint string) error {

	// Setup graceful shutdown - listen for both SIGINT and SIGTERM
	shutdownCtx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Create server context
	serverContext, err := server.NewServerContext(shutdownCtx,
		server.WithDebugMode(debugMode),
		server.WithLogger(&simpleLogger{}),
	)
	if err != nil {
		return fmt.Errorf("failed to create server context: %w", err)
	}
	defer func() {
		if err := serverContext.Shutdown(); err != nil {
			log.Printf("Error during server context shutdown: %v", err)
		}
	}()

	// Log configuration
	config := serverContext.PrometheusConfig()
	fmt.Printf("Prometheus configuration:\n")
	fmt.Printf("  Server URL: %s\n", config.URL)
	if config.Username != "" && config.Password != "" {
		fmt.Printf("  Authentication: Basic auth (username: %s)\n", config.Username)
	} else if config.Token != "" {
		fmt.Printf("  Authentication: Bearer token\n")
	} else {
		fmt.Printf("  Authentication: None\n")
	}
	if config.OrgID != "" {
		fmt.Printf("  Organization ID: %s\n", config.OrgID)
	}

	// Create MCP server
	mcpSrv := mcpserver.NewMCPServer("mcp-prometheus", rootCmd.Version,
		mcpserver.WithToolCapabilities(true),
	)

	// Register Prometheus tools
	if err := prometheus.RegisterPrometheusTools(mcpSrv, serverContext); err != nil {
		return fmt.Errorf("failed to register Prometheus tools: %w", err)
	}

	fmt.Printf("Starting MCP Prometheus server with %s transport...\n", transport)

	// Start the appropriate server based on transport type
	switch transport {
	case "stdio":
		return runStdioServer(mcpSrv)
	case "sse":
		return runSSEServer(mcpSrv, httpAddr, sseEndpoint, messageEndpoint, shutdownCtx, debugMode)
	case "streamable-http":
		return runStreamableHTTPServer(mcpSrv, httpAddr, httpEndpoint, shutdownCtx, debugMode)
	default:
		return fmt.Errorf("unsupported transport type: %s (supported: stdio, sse, streamable-http)", transport)
	}
}

// runStdioServer runs the server with STDIO transport
func runStdioServer(mcpSrv *mcpserver.MCPServer) error {
	// Start the server in a goroutine so we can handle shutdown signals
	serverDone := make(chan error, 1)
	go func() {
		defer close(serverDone)
		if err := mcpserver.ServeStdio(mcpSrv); err != nil {
			serverDone <- err
		}
	}()

	// Wait for server completion
	select {
	case err := <-serverDone:
		if err != nil {
			return fmt.Errorf("server stopped with error: %w", err)
		} else {
			fmt.Println("Server stopped normally")
		}
	}

	fmt.Println("Server gracefully stopped")
	return nil
}

// runSSEServer runs the server with SSE transport
func runSSEServer(mcpSrv *mcpserver.MCPServer, addr, sseEndpoint, messageEndpoint string, ctx context.Context, debugMode bool) error {
	if debugMode {
		log.Printf("[DEBUG] Initializing SSE server with configuration:")
		log.Printf("[DEBUG]   Address: %s", addr)
		log.Printf("[DEBUG]   SSE Endpoint: %s", sseEndpoint)
		log.Printf("[DEBUG]   Message Endpoint: %s", messageEndpoint)
	}

	// Create SSE server with custom endpoints
	sseServer := mcpserver.NewSSEServer(mcpSrv,
		mcpserver.WithSSEEndpoint(sseEndpoint),
		mcpserver.WithMessageEndpoint(messageEndpoint),
	)

	if debugMode {
		log.Printf("[DEBUG] SSE server instance created successfully")
	}

	fmt.Printf("SSE server starting on %s\n", addr)
	fmt.Printf("  SSE endpoint: %s\n", sseEndpoint)
	fmt.Printf("  Message endpoint: %s\n", messageEndpoint)

	// Start server in goroutine
	serverDone := make(chan error, 1)
	go func() {
		defer close(serverDone)
		if debugMode {
			log.Printf("[DEBUG] Starting SSE server listener on %s", addr)
		}
		if err := sseServer.Start(addr); err != nil {
			if debugMode {
				log.Printf("[DEBUG] SSE server start failed: %v", err)
			}
			serverDone <- err
		} else {
			if debugMode {
				log.Printf("[DEBUG] SSE server listener stopped cleanly")
			}
		}
	}()

	if debugMode {
		log.Printf("[DEBUG] SSE server goroutine started, waiting for shutdown signal or server completion")
	}

	// Wait for either shutdown signal or server completion
	select {
	case <-ctx.Done():
		if debugMode {
			log.Printf("[DEBUG] Shutdown signal received, initiating SSE server shutdown")
		}
		fmt.Println("Shutdown signal received, stopping SSE server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30)
		defer cancel()
		if debugMode {
			log.Printf("[DEBUG] Starting graceful shutdown with 30s timeout")
		}
		if err := sseServer.Shutdown(shutdownCtx); err != nil {
			if debugMode {
				log.Printf("[DEBUG] Error during SSE server shutdown: %v", err)
			}
			return fmt.Errorf("error shutting down SSE server: %w", err)
		}
		if debugMode {
			log.Printf("[DEBUG] SSE server shutdown completed successfully")
		}
	case err := <-serverDone:
		if err != nil {
			if debugMode {
				log.Printf("[DEBUG] SSE server stopped with error: %v", err)
			}
			return fmt.Errorf("SSE server stopped with error: %w", err)
		} else {
			if debugMode {
				log.Printf("[DEBUG] SSE server stopped normally")
			}
			fmt.Println("SSE server stopped normally")
		}
	}

	fmt.Println("SSE server gracefully stopped")
	if debugMode {
		log.Printf("[DEBUG] SSE server shutdown sequence completed")
	}
	return nil
}

// runStreamableHTTPServer runs the server with Streamable HTTP transport
func runStreamableHTTPServer(mcpSrv *mcpserver.MCPServer, addr, endpoint string, ctx context.Context, debugMode bool) error {
	// Create Streamable HTTP server with custom endpoint
	httpServer := mcpserver.NewStreamableHTTPServer(mcpSrv,
		mcpserver.WithEndpointPath(endpoint),
	)

	fmt.Printf("Streamable HTTP server starting on %s\n", addr)
	fmt.Printf("  HTTP endpoint: %s\n", endpoint)

	// Start server in goroutine
	serverDone := make(chan error, 1)
	go func() {
		defer close(serverDone)
		if err := httpServer.Start(addr); err != nil {
			serverDone <- err
		}
	}()

	// Wait for either shutdown signal or server completion
	select {
	case <-ctx.Done():
		fmt.Println("Shutdown signal received, stopping HTTP server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("error shutting down HTTP server: %w", err)
		}
	case err := <-serverDone:
		if err != nil {
			return fmt.Errorf("HTTP server stopped with error: %w", err)
		} else {
			fmt.Println("HTTP server stopped normally")
		}
	}

	fmt.Println("HTTP server gracefully stopped")
	return nil
}
