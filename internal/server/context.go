package server

import (
	"context"
	"os"
	"sync"
)

// Logger interface for structured logging
type Logger interface {
	Debug(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

// PrometheusConfig holds the Prometheus server configuration
type PrometheusConfig struct {
	URL      string
	Username string
	Password string
	Token    string
	OrgID    string

	// TLS configuration
	TLSSkipVerify bool   // PROMETHEUS_TLS_SKIP_VERIFY — disable TLS certificate verification
	TLSCACert     string // PROMETHEUS_TLS_CA_CERT — path to a PEM-encoded CA certificate file
}

// TenancyResolver resolves Mimir tenant IDs from a set of authenticated user
// groups.  It is implemented by [tenancy.Resolver] and exposed here as an
// interface to avoid a direct import cycle between the server and tenancy
// packages.
type TenancyResolver interface {
	TenantsForGroups(ctx context.Context, groups []string) ([]string, error)
}

// ServerContext holds the server configuration and shared resources
type ServerContext struct {
	ctx    context.Context
	cancel context.CancelFunc
	mutex  sync.RWMutex

	// Configuration
	debugMode bool
	logger    Logger

	// Prometheus configuration
	prometheusConfig PrometheusConfig

	// OAuth / tenancy (optional; nil when disabled)
	oauthEnabled    bool
	tenancyResolver TenancyResolver
}

// ServerOption is a functional option for configuring ServerContext
type ServerOption func(*ServerContext)

// WithDebugMode sets whether debug logging is enabled
func WithDebugMode(enabled bool) ServerOption {
	return func(sc *ServerContext) {
		sc.debugMode = enabled
	}
}

// WithLogger sets the logger for the server context
func WithLogger(logger Logger) ServerOption {
	return func(sc *ServerContext) {
		sc.logger = logger
	}
}

// WithPrometheusConfig sets the Prometheus configuration
func WithPrometheusConfig(config PrometheusConfig) ServerOption {
	return func(sc *ServerContext) {
		sc.prometheusConfig = config
	}
}

// WithOAuthEnabled marks the server as running behind OAuth 2.1 middleware.
// When true, tool handlers will attempt to extract user info from the request
// context to perform tenancy resolution.
func WithOAuthEnabled(enabled bool) ServerOption {
	return func(sc *ServerContext) {
		sc.oauthEnabled = enabled
	}
}

// WithTenancyResolver attaches a tenancy resolver used by tools to map
// authenticated user groups to Mimir X-Scope-OrgID values.
func WithTenancyResolver(r TenancyResolver) ServerOption {
	return func(sc *ServerContext) {
		sc.tenancyResolver = r
	}
}

// NewServerContext creates a new server context with the given options
func NewServerContext(ctx context.Context, opts ...ServerOption) (*ServerContext, error) {
	serverCtx, cancel := context.WithCancel(ctx)

	sc := &ServerContext{
		ctx:    serverCtx,
		cancel: cancel,
	}

	// Apply options
	for _, opt := range opts {
		opt(sc)
	}

	// Set default logger if none provided
	if sc.logger == nil {
		sc.logger = &noopLogger{}
	}

	// Load Prometheus configuration from environment if not provided
	if sc.prometheusConfig.URL == "" {
		sc.prometheusConfig = PrometheusConfig{
			URL:           os.Getenv("PROMETHEUS_URL"),
			Username:      os.Getenv("PROMETHEUS_USERNAME"),
			Password:      os.Getenv("PROMETHEUS_PASSWORD"),
			Token:         os.Getenv("PROMETHEUS_TOKEN"),
			OrgID:         os.Getenv("PROMETHEUS_ORGID"),
			TLSSkipVerify: os.Getenv("PROMETHEUS_TLS_SKIP_VERIFY") == "true",
			TLSCACert:     os.Getenv("PROMETHEUS_TLS_CA_CERT"),
		}
	}

	return sc, nil
}

// Context returns the context associated with the server
func (sc *ServerContext) Context() context.Context {
	sc.mutex.RLock()
	defer sc.mutex.RUnlock()
	return sc.ctx
}

// IsDebugMode returns whether debug logging is enabled
func (sc *ServerContext) IsDebugMode() bool {
	sc.mutex.RLock()
	defer sc.mutex.RUnlock()
	return sc.debugMode
}

// Logger returns the configured logger
func (sc *ServerContext) Logger() Logger {
	sc.mutex.RLock()
	defer sc.mutex.RUnlock()
	return sc.logger
}

// PrometheusConfig returns the Prometheus configuration
func (sc *ServerContext) PrometheusConfig() PrometheusConfig {
	sc.mutex.RLock()
	defer sc.mutex.RUnlock()
	return sc.prometheusConfig
}

// IsOAuthEnabled returns whether OAuth 2.1 middleware is active.
func (sc *ServerContext) IsOAuthEnabled() bool {
	sc.mutex.RLock()
	defer sc.mutex.RUnlock()
	return sc.oauthEnabled
}

// TenancyResolver returns the configured tenancy resolver, or nil when tenancy
// is disabled.
func (sc *ServerContext) TenancyResolver() TenancyResolver {
	sc.mutex.RLock()
	defer sc.mutex.RUnlock()
	return sc.tenancyResolver
}

// SetDebugMode dynamically sets whether debug logging is enabled
func (sc *ServerContext) SetDebugMode(enabled bool) {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	sc.debugMode = enabled
}

// Shutdown gracefully shuts down the server context
func (sc *ServerContext) Shutdown() error {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()

	if sc.cancel != nil {
		sc.cancel()
		sc.cancel = nil
	}

	return nil
}

// noopLogger is a logger that does nothing
type noopLogger struct{}

func (l *noopLogger) Debug(msg string, args ...interface{}) {}
func (l *noopLogger) Info(msg string, args ...interface{})  {}
func (l *noopLogger) Warn(msg string, args ...interface{})  {}
func (l *noopLogger) Error(msg string, args ...interface{}) {}
