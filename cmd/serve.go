package cmd

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	mcpoauth "github.com/giantswarm/mcp-oauth"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"

	"github.com/giantswarm/mcp-prometheus/internal/oauth"
	"github.com/giantswarm/mcp-prometheus/internal/observability"
	"github.com/giantswarm/mcp-prometheus/internal/server"
	"github.com/giantswarm/mcp-prometheus/internal/tenancy"
	"github.com/giantswarm/mcp-prometheus/internal/tools/prometheus"
)

// simpleLogger provides basic logging for the server
type simpleLogger struct{}

func (l *simpleLogger) Debug(msg string, args ...any) {
	log.Printf("[DEBUG] %s %v", msg, args)
}

func (l *simpleLogger) Info(msg string, args ...any) {
	log.Printf("[INFO] %s %v", msg, args)
}

func (l *simpleLogger) Warn(msg string, args ...any) {
	log.Printf("[WARN] %s %v", msg, args)
}

func (l *simpleLogger) Error(msg string, args ...any) {
	log.Printf("[ERROR] %s %v", msg, args)
}

// newServeCmd creates the Cobra command for starting the MCP server.
func newServeCmd() *cobra.Command {
	var (
		debugMode   bool
		enableOAuth bool

		// Transport options
		transport       string
		httpAddr        string
		sseEndpoint     string
		messageEndpoint string
		httpEndpoint    string

		// Observability
		metricsAddr string
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

OAuth 2.1 (when --enable-oauth is set):
  MCP_OAUTH_ISSUER              - OAuth issuer URL (required)
  MCP_OAUTH_ENCRYPTION_KEY      - AES-256-GCM key for token encryption (hex, required)
  OAUTH_STORAGE                 - Storage backend: memory (default) or valkey
  VALKEY_URL                    - Valkey connection URL (when OAUTH_STORAGE=valkey)
  DEX_ISSUER_URL                - Dex OIDC issuer URL (required)
  DEX_CLIENT_ID                 - Dex OAuth client ID (required)
  DEX_CLIENT_SECRET             - Dex OAuth client secret (required)
  DEX_REDIRECT_URL              - OAuth redirect URL (required)

If PROMETHEUS_URL or PROMETHEUS_ORGID environment variables are not set,
they can be provided as parameters to individual tool calls.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(transport, debugMode, enableOAuth,
				httpAddr, sseEndpoint, messageEndpoint, httpEndpoint,
				metricsAddr)
		},
	}

	// Add flags for configuring the server
	cmd.Flags().BoolVar(&debugMode, "debug", false, "Enable debug logging (default: false)")
	cmd.Flags().BoolVar(&enableOAuth, "enable-oauth", false, "Enable OAuth 2.1 authentication (requires MCP_OAUTH_* and DEX_* env vars; sse/streamable-http only)")

	// Transport flags
	cmd.Flags().StringVar(&transport, "transport", "stdio", "Transport type: stdio, sse, or streamable-http")
	cmd.Flags().StringVar(&httpAddr, "http-addr", ":8080", "HTTP server address (for sse and streamable-http transports)")
	cmd.Flags().StringVar(&sseEndpoint, "sse-endpoint", "/sse", "SSE endpoint path (for sse transport)")
	cmd.Flags().StringVar(&messageEndpoint, "message-endpoint", "/message", "Message endpoint path (for sse transport)")
	cmd.Flags().StringVar(&httpEndpoint, "http-endpoint", "/mcp", "HTTP endpoint path (for streamable-http transport)")
	cmd.Flags().StringVar(&metricsAddr, "metrics-addr", ":9091", "Address for the observability HTTP server (/metrics, /healthz, /readyz). Set to empty string to disable.")

	return cmd
}

// runServe contains the main server logic with support for multiple transports
func runServe(transport string, debugMode bool, enableOAuth bool,
	httpAddr, sseEndpoint, messageEndpoint, httpEndpoint string,
	metricsAddr string) error {

	// Setup graceful shutdown - listen for both SIGINT and SIGTERM
	shutdownCtx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Collect server context options; OAuth may append more below.
	serverOpts := []server.ServerOption{
		server.WithDebugMode(debugMode),
		server.WithLogger(&simpleLogger{}),
	}

	// OAuth 2.1 setup (SSE and streamable-http transports only).
	var oauthHandler *mcpoauth.Handler
	if enableOAuth {
		if transport == "stdio" {
			return fmt.Errorf("--enable-oauth is not supported with stdio transport")
		}

		logLevel := slog.LevelInfo
		if debugMode {
			logLevel = slog.LevelDebug
		}
		slogLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

		oauthCfg := oauth.ConfigFromEnv()
		h, cleanup, err := oauth.NewHandler(shutdownCtx, oauthCfg, slogLogger)
		if err != nil {
			return fmt.Errorf("failed to initialise OAuth handler: %w", err)
		}
		defer cleanup()
		oauthHandler = h

		tenancyResolver, err := tenancy.NewInClusterResolver()
		if err != nil {
			return fmt.Errorf("failed to create tenancy resolver: %w", err)
		}

		serverOpts = append(serverOpts,
			server.WithOAuthEnabled(true),
			server.WithTenancyResolver(tenancyResolver),
		)

		fmt.Println("OAuth 2.1 authentication enabled")
	}

	// Create server context
	serverContext, err := server.NewServerContext(shutdownCtx, serverOpts...)
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

	// Initialise observability (metrics + tracing)
	metrics := observability.NewMetrics()
	health := &observability.Health{}

	tp, shutdownTracer, err := observability.NewTracerProvider(shutdownCtx)
	if err != nil {
		return fmt.Errorf("failed to initialise OTel tracer: %w", err)
	}
	defer func() {
		if err := shutdownTracer(context.Background()); err != nil {
			log.Printf("Error flushing OTel spans: %v", err)
		}
	}()

	inst := observability.NewInstrumentor(metrics, tp)

	// Start observability HTTP server (/metrics, /healthz, /readyz) unless disabled.
	// Listen is called synchronously so a port-conflict error fails startup immediately.
	if metricsAddr != "" {
		mux := observability.NewServer(metrics, health)
		ln, err := observability.Listen(metricsAddr)
		if err != nil {
			return fmt.Errorf("failed to bind observability server: %w", err)
		}
		fmt.Printf("Observability server listening on %s\n", metricsAddr)
		go func() {
			if err := observability.Serve(shutdownCtx, ln, mux); err != nil {
				log.Printf("Observability server stopped: %v", err)
			}
		}()
	}

	// Create MCP server
	mcpSrv := mcpserver.NewMCPServer("mcp-prometheus", rootCmd.Version,
		mcpserver.WithToolCapabilities(true),
	)

	// Register Prometheus tools with observability instrumentation
	if err := prometheus.RegisterPrometheusTools(mcpSrv, serverContext, inst.Wrap); err != nil {
		return fmt.Errorf("failed to register Prometheus tools: %w", err)
	}

	// Mark server as ready after all tools are registered
	health.SetReady(true)

	fmt.Printf("Starting MCP Prometheus server with %s transport...\n", transport)

	// Start the appropriate server based on transport type
	switch transport {
	case "stdio":
		return runStdioServer(mcpSrv)
	case "sse":
		return runSSEServer(mcpSrv, httpAddr, sseEndpoint, messageEndpoint, shutdownCtx, debugMode, oauthHandler)
	case "streamable-http":
		return runStreamableHTTPServer(mcpSrv, httpAddr, httpEndpoint, shutdownCtx, debugMode, oauthHandler)
	default:
		return fmt.Errorf("unsupported transport type: %s (supported: stdio, sse, streamable-http)", transport)
	}
}

// serveHTTPWithShutdown binds a TCP listener, serves with the given handler, and
// shuts down gracefully when ctx is cancelled.
func serveHTTPWithShutdown(addr string, handler http.Handler, ctx context.Context) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	srv := &http.Server{Handler: handler}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	case err := <-errCh:
		return err
	}
}

// registerOAuthRoutes mounts all OAuth 2.1 endpoints onto mux and wraps each
// MCP handler with ValidateToken middleware.
func registerOAuthRoutes(mux *http.ServeMux, h *mcpoauth.Handler, mcpPath string, mcpHandlers map[string]http.Handler) {
	h.RegisterAuthorizationServerMetadataRoutes(mux)
	h.RegisterProtectedResourceMetadataRoutes(mux, mcpPath)
	mux.HandleFunc("/oauth/authorize", h.ServeAuthorization)
	mux.HandleFunc("/oauth/callback", h.ServeCallback)
	mux.HandleFunc("/oauth/token", h.ServeToken)
	mux.HandleFunc("/oauth/register", h.ServeClientRegistration)
	mux.HandleFunc("/oauth/revoke", h.ServeTokenRevocation)
	for path, handler := range mcpHandlers {
		mux.Handle(path, h.ValidateToken(handler))
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

// runSSEServer runs the server with SSE transport.
// When oauthHandler is non-nil, a custom HTTP mux is built with OAuth 2.1
// endpoints and the SSE/message paths are protected with ValidateToken.
func runSSEServer(mcpSrv *mcpserver.MCPServer, addr, sseEndpoint, messageEndpoint string, ctx context.Context, debugMode bool, oauthHandler *mcpoauth.Handler) error {
	if debugMode {
		log.Printf("[DEBUG] SSE server: addr=%s sse=%s message=%s oauth=%v", addr, sseEndpoint, messageEndpoint, oauthHandler != nil)
	}

	sseServer := mcpserver.NewSSEServer(mcpSrv,
		mcpserver.WithSSEEndpoint(sseEndpoint),
		mcpserver.WithMessageEndpoint(messageEndpoint),
	)

	fmt.Printf("SSE server starting on %s (sse=%s message=%s)\n", addr, sseEndpoint, messageEndpoint)

	if oauthHandler != nil {
		// OAuth path: custom mux with protected MCP routes.
		mux := http.NewServeMux()
		registerOAuthRoutes(mux, oauthHandler, sseEndpoint, map[string]http.Handler{
			sseEndpoint:     sseServer.SSEHandler(),
			messageEndpoint: sseServer.MessageHandler(),
		})
		fmt.Println("  OAuth 2.1 endpoints mounted at /oauth/*")
		return serveHTTPWithShutdown(addr, mux, ctx)
	}

	// No OAuth: use the SSE server's built-in Start/Shutdown lifecycle.
	serverDone := make(chan error, 1)
	go func() {
		defer close(serverDone)
		if err := sseServer.Start(addr); err != nil {
			serverDone <- err
		}
	}()

	select {
	case <-ctx.Done():
		fmt.Println("Shutdown signal received, stopping SSE server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := sseServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("error shutting down SSE server: %w", err)
		}
	case err := <-serverDone:
		if err != nil {
			return fmt.Errorf("SSE server stopped with error: %w", err)
		}
	}

	fmt.Println("SSE server gracefully stopped")
	return nil
}

// runStreamableHTTPServer runs the server with Streamable HTTP transport.
// When oauthHandler is non-nil, MCP requests are protected with ValidateToken.
func runStreamableHTTPServer(mcpSrv *mcpserver.MCPServer, addr, endpoint string, ctx context.Context, debugMode bool, oauthHandler *mcpoauth.Handler) error {
	httpServer := mcpserver.NewStreamableHTTPServer(mcpSrv,
		mcpserver.WithEndpointPath(endpoint),
	)

	fmt.Printf("Streamable HTTP server starting on %s (endpoint=%s)\n", addr, endpoint)

	if oauthHandler != nil {
		// OAuth path: custom mux with protected MCP route.
		mux := http.NewServeMux()
		registerOAuthRoutes(mux, oauthHandler, endpoint, map[string]http.Handler{
			endpoint: httpServer,
		})
		fmt.Println("  OAuth 2.1 endpoints mounted at /oauth/*")
		return serveHTTPWithShutdown(addr, mux, ctx)
	}

	// No OAuth: use the streamable HTTP server's built-in lifecycle.
	serverDone := make(chan error, 1)
	go func() {
		defer close(serverDone)
		if err := httpServer.Start(addr); err != nil {
			serverDone <- err
		}
	}()

	select {
	case <-ctx.Done():
		fmt.Println("Shutdown signal received, stopping HTTP server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("error shutting down HTTP server: %w", err)
		}
	case err := <-serverDone:
		if err != nil {
			return fmt.Errorf("HTTP server stopped with error: %w", err)
		}
	}

	fmt.Println("HTTP server gracefully stopped")
	return nil
}
