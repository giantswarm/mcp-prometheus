package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	mcpoauth "github.com/giantswarm/mcp-oauth"
	"github.com/giantswarm/mcp-toolkit/health"
	"github.com/giantswarm/mcp-toolkit/httpx"
	"github.com/giantswarm/mcp-toolkit/logging"
	"github.com/giantswarm/mcp-toolkit/middleware/responsecap"
	"github.com/giantswarm/mcp-toolkit/middleware/timeout"
	"github.com/giantswarm/mcp-toolkit/tracing"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/giantswarm/mcp-prometheus/internal/oauth"
	"github.com/giantswarm/mcp-prometheus/internal/observability"
	"github.com/giantswarm/mcp-prometheus/internal/server"
	"github.com/giantswarm/mcp-prometheus/internal/tenancy"
	"github.com/giantswarm/mcp-prometheus/internal/tools/prometheus"
)

// serviceName is the OTEL service.name and the MCP server identifier.
// Override at build time via -ldflags "-X github.com/giantswarm/mcp-prometheus/cmd.serviceName=...".
var serviceName = "mcp-prometheus"

const (
	transportStdio          = "stdio"
	transportSSE            = "sse"
	transportStreamableHTTP = "streamable-http"
)

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

		// Tenancy
		tenancyMode   string
		staticTenants string
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
  MCP_OAUTH_ENCRYPTION_KEY      - AES-256-GCM key for token encryption (base64, required)
  OAUTH_STORAGE                 - Storage backend: memory (default) or valkey
  VALKEY_URL                    - Valkey connection URL (when OAUTH_STORAGE=valkey)
  DEX_ISSUER_URL                - Dex OIDC issuer URL (required)
  DEX_CLIENT_ID                 - Dex OAuth client ID (required)
  DEX_CLIENT_SECRET             - Dex OAuth client secret (required)
  DEX_REDIRECT_URL              - OAuth redirect URL (required)

Tenancy (when --enable-oauth is set):
  --tenancy-mode                - grafana-organization (default) or static
  --static-tenants              - Comma-separated tenant IDs for all users (static mode)
  TENANCY_STATIC_GROUP_MAP      - JSON map of group→[tenant IDs] for group-mapping (static mode)

If PROMETHEUS_URL or PROMETHEUS_ORGID environment variables are not set,
they can be provided as parameters to individual tool calls.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(transport, debugMode, enableOAuth,
				httpAddr, sseEndpoint, messageEndpoint, httpEndpoint,
				metricsAddr, tenancyMode, staticTenants)
		},
	}

	cmd.Flags().BoolVar(&debugMode, "debug", false, "Enable debug logging (default: false)")
	cmd.Flags().BoolVar(&enableOAuth, "enable-oauth", false, "Enable OAuth 2.1 authentication (requires MCP_OAUTH_* and DEX_* env vars; sse/streamable-http only)")

	cmd.Flags().StringVar(&transport, "transport", transportStdio,
		fmt.Sprintf("Transport type: %s, %s, or %s", transportStdio, transportSSE, transportStreamableHTTP))
	cmd.Flags().StringVar(&httpAddr, "http-addr", ":8080", "HTTP server address (for sse and streamable-http transports)")
	cmd.Flags().StringVar(&sseEndpoint, "sse-endpoint", "/sse", "SSE endpoint path (for sse transport)")
	cmd.Flags().StringVar(&messageEndpoint, "message-endpoint", "/message", "Message endpoint path (for sse transport)")
	cmd.Flags().StringVar(&httpEndpoint, "http-endpoint", "/mcp", "HTTP endpoint path (for streamable-http transport)")
	cmd.Flags().StringVar(&metricsAddr, "metrics-addr", ":9091", "Address for the observability HTTP server (/metrics, /healthz, /readyz). Set to empty string to disable.")

	cmd.Flags().StringVar(&tenancyMode, "tenancy-mode", string(tenancy.ModeGrafanaOrganization),
		"Tenancy resolution mode when OAuth is enabled: grafana-organization or static")
	cmd.Flags().StringVar(&staticTenants, "static-tenants", "",
		"Comma-separated Mimir tenant IDs for all authenticated users (--tenancy-mode=static only)")

	return cmd
}

// runServe contains the main server logic with support for multiple transports
func runServe(transport string, debugMode bool, enableOAuth bool,
	httpAddr, sseEndpoint, messageEndpoint, httpEndpoint string,
	metricsAddr string, tenancyMode string, staticTenants string) error {

	logLevel := slog.LevelInfo
	if debugMode {
		logLevel = slog.LevelDebug
	}
	logger := logging.New(logging.Options{Level: logLevel})

	shutdownCtx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Best-effort OTEL tracing. No-op when no OTLP endpoint is configured.
	shutdownOTEL, err := tracing.Init(shutdownCtx, serviceName, rootCmd.Version)
	if err != nil {
		logger.Warn("otel init failed; continuing without tracing", "error", err)
	} else {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = shutdownOTEL(ctx)
		}()
	}

	serverOpts := []server.ServerOption{
		server.WithSlogLogger(logger),
	}

	var oauthHandler *mcpoauth.Handler
	if enableOAuth {
		if transport == transportStdio {
			return fmt.Errorf("--enable-oauth is not supported with stdio transport")
		}

		oauthCfg := oauth.ConfigFromEnv()
		h, cleanup, err := oauth.NewHandler(shutdownCtx, oauthCfg, logger)
		if err != nil {
			return fmt.Errorf("failed to initialise OAuth handler: %w", err)
		}
		defer cleanup()
		oauthHandler = h

		var groupMap map[string][]string
		if raw := os.Getenv("TENANCY_STATIC_GROUP_MAP"); raw != "" {
			if err := json.Unmarshal([]byte(raw), &groupMap); err != nil {
				return fmt.Errorf("TENANCY_STATIC_GROUP_MAP: invalid JSON: %w", err)
			}
		}

		tenancyResolver, err := tenancy.NewResolverForMode(
			tenancy.Mode(tenancyMode),
			parseTenants(staticTenants),
			groupMap,
		)
		if err != nil {
			return fmt.Errorf("failed to create tenancy resolver: %w", err)
		}

		serverOpts = append(serverOpts,
			server.WithOAuthEnabled(true),
			server.WithTenancyResolver(tenancyResolver),
		)

		logger.Info("OAuth 2.1 authentication enabled", "tenancy_mode", tenancyMode)
	}

	serverContext, err := server.NewServerContext(shutdownCtx, serverOpts...)
	if err != nil {
		return fmt.Errorf("failed to create server context: %w", err)
	}
	defer func() {
		if err := serverContext.Shutdown(); err != nil {
			logger.Error("Error during server context shutdown", "error", err)
		}
	}()

	config := serverContext.PrometheusConfig()
	authMethod := "none"
	if config.Username != "" && config.Password != "" {
		authMethod = fmt.Sprintf("basic (username: %s)", config.Username)
	} else if config.Token != "" {
		authMethod = "bearer token"
	}
	logger.Info("Prometheus configuration",
		"url", config.URL,
		"auth", authMethod,
		"org_id", config.OrgID,
	)

	metrics := observability.NewMetrics()
	hc := health.New()

	inst := observability.NewInstrumentor(metrics, otelTracerProvider())

	// Observability HTTP server (/metrics, /healthz, /readyz). Disabled when
	// metricsAddr is empty (e.g. tests, stdio CLI use). Bind happens in a
	// goroutine — port-conflict errors propagate via the shutdown ctx.
	if metricsAddr != "" {
		obsMux := http.NewServeMux()
		obsMux.Handle("/metrics", metrics.Handler())
		hc.Mount(obsMux)
		obsServer := &http.Server{
			Addr:              metricsAddr,
			Handler:           obsMux,
			ReadHeaderTimeout: 5 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
		logger.Info("Observability server listening", "addr", metricsAddr)
		go func() {
			if err := httpx.Run(shutdownCtx, obsServer, 5*time.Second); err != nil {
				logger.Error("Observability server stopped", "error", err)
				cancel()
			}
		}()
	}

	mcpSrv := mcpserver.NewMCPServer(serviceName, rootCmd.Version,
		mcpserver.WithToolCapabilities(true),
		mcpserver.WithInputSchemaValidation(),
		mcpserver.WithToolHandlerMiddleware(timeout.New(30*time.Second)),
		mcpserver.WithToolHandlerMiddleware(responsecap.New(responsecap.Options{
			Limit:         prometheus.MaxResultLength,
			Exempt:        prometheus.IsExempt,
			AllowOverride: prometheus.IsBypass,
			Hint:          prometheus.HintFor,
		})),
	)

	if err := prometheus.RegisterPrometheusTools(mcpSrv, serverContext, inst.Wrap); err != nil {
		return fmt.Errorf("failed to register Prometheus tools: %w", err)
	}

	hc.SetReady(true)

	logger.Info("Starting MCP Prometheus server", "transport", transport)

	switch transport {
	case transportStdio:
		return runStdioServer(mcpSrv, logger)
	case transportSSE:
		return runSSEServer(mcpSrv, httpAddr, sseEndpoint, messageEndpoint, shutdownCtx, logger, oauthHandler)
	case transportStreamableHTTP:
		return runStreamableHTTPServer(mcpSrv, httpAddr, httpEndpoint, shutdownCtx, logger, oauthHandler)
	default:
		return fmt.Errorf("unsupported transport type: %s (supported: %s, %s, %s)",
			transport, transportStdio, transportSSE, transportStreamableHTTP)
	}
}

// otelTracerProvider returns the global OTel TracerProvider. The toolkit's
// tracing.Init has either installed a real provider or left the noop default
// in place; either way otel.GetTracerProvider() returns the right thing.
func otelTracerProvider() trace.TracerProvider {
	return otel.GetTracerProvider()
}

// parseTenants splits a comma-separated tenant string into a trimmed, non-empty slice.
func parseTenants(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			result = append(result, p)
		}
	}
	return result
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
func runStdioServer(mcpSrv *mcpserver.MCPServer, logger *slog.Logger) error {
	if err := mcpserver.ServeStdio(mcpSrv); err != nil {
		return fmt.Errorf("server stopped with error: %w", err)
	}
	logger.Info("Server gracefully stopped")
	return nil
}

// runSSEServer runs the server with SSE transport.
// When oauthHandler is non-nil, a custom HTTP mux is built with OAuth 2.1
// endpoints and the SSE/message paths are protected with ValidateToken.
func runSSEServer(mcpSrv *mcpserver.MCPServer, addr, sseEndpoint, messageEndpoint string, ctx context.Context, logger *slog.Logger, oauthHandler *mcpoauth.Handler) error {
	logger.Debug("SSE server configuration", "addr", addr, "sse_endpoint", sseEndpoint, "message_endpoint", messageEndpoint, "oauth", oauthHandler != nil)

	sseServer := mcpserver.NewSSEServer(mcpSrv,
		mcpserver.WithSSEEndpoint(sseEndpoint),
		mcpserver.WithMessageEndpoint(messageEndpoint),
	)

	logger.Info("SSE server starting", "addr", addr, "sse_endpoint", sseEndpoint, "message_endpoint", messageEndpoint)

	mux := http.NewServeMux()
	if oauthHandler != nil {
		registerOAuthRoutes(mux, oauthHandler, sseEndpoint, map[string]http.Handler{
			sseEndpoint:     sseServer.SSEHandler(),
			messageEndpoint: sseServer.MessageHandler(),
		})
		logger.Info("OAuth 2.1 endpoints mounted", "path", "/oauth/*")
	} else {
		mux.Handle(sseEndpoint, sseServer)
		mux.Handle(messageEndpoint, sseServer)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return httpx.Run(ctx, srv, 30*time.Second)
}

// runStreamableHTTPServer runs the server with Streamable HTTP transport.
// When oauthHandler is non-nil, MCP requests are protected with ValidateToken.
func runStreamableHTTPServer(mcpSrv *mcpserver.MCPServer, addr, endpoint string, ctx context.Context, logger *slog.Logger, oauthHandler *mcpoauth.Handler) error {
	httpServer := mcpserver.NewStreamableHTTPServer(mcpSrv,
		mcpserver.WithEndpointPath(endpoint),
	)

	logger.Info("Streamable HTTP server starting", "addr", addr, "endpoint", endpoint)

	mux := http.NewServeMux()
	if oauthHandler != nil {
		registerOAuthRoutes(mux, oauthHandler, endpoint, map[string]http.Handler{
			endpoint: httpServer,
		})
		logger.Info("OAuth 2.1 endpoints mounted", "path", "/oauth/*")
	} else {
		mux.Handle(endpoint, httpServer)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return httpx.Run(ctx, srv, 30*time.Second)
}
