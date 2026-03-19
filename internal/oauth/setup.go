package oauth

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	mcpoauth "github.com/giantswarm/mcp-oauth"
	"github.com/giantswarm/mcp-oauth/providers"
	"github.com/giantswarm/mcp-oauth/providers/dex"
	mcpoidc "github.com/giantswarm/mcp-oauth/providers/oidc"
	"github.com/giantswarm/mcp-oauth/security"
	"github.com/giantswarm/mcp-oauth/storage"
	"github.com/giantswarm/mcp-oauth/storage/memory"
	"github.com/giantswarm/mcp-oauth/storage/valkey"
)

// Config holds all configuration needed to start the OAuth 2.1 Authorization Server.
// Every field maps directly to an environment variable so the server can be configured
// without code changes.
type Config struct {
	// Issuer is the public base URL of this MCP server (e.g. "https://mcp.example.com").
	// Used as the OAuth issuer identifier and as the base for endpoint URLs.
	Issuer string

	// EncryptionKey is a 32-byte hex-encoded key used for AES-256-GCM token encryption.
	// If empty, tokens are stored unencrypted (only suitable for development).
	EncryptionKey string

	// AllowPublicRegistration permits unauthenticated dynamic client registration.
	// Set true for development / MCP Inspector; false for production.
	AllowPublicRegistration bool

	// StorageType selects the token store backend: "memory" (default) or "valkey".
	StorageType string

	// ValkeyURL is the Valkey/Redis address, e.g. "localhost:6379".
	// Required when StorageType == "valkey".
	ValkeyURL string

	// ValkeyPassword is the optional Valkey authentication password.
	ValkeyPassword string

	// ValkeyTLS enables TLS for Valkey connections.
	ValkeyTLS bool

	// ValkeyKeyPrefix is an optional key namespace prefix (default: "mcp:").
	ValkeyKeyPrefix string

	// DexIssuerURL is the Dex OIDC issuer, e.g. "https://dex.example.com".
	DexIssuerURL string

	// DexClientID is the OAuth client ID registered in Dex.
	DexClientID string

	// DexClientSecret is the OAuth client secret registered in Dex.
	DexClientSecret string

	// DexRedirectURL is the callback URL registered in Dex,
	// e.g. "https://mcp.example.com/oauth/callback".
	DexRedirectURL string

	// TrustedAudiences lists OAuth client IDs whose tokens are accepted for SSO.
	// When an upstream aggregator (like muster) forwards a user's ID token,
	// mcp-prometheus accepts it if the token's audience matches any entry here.
	// Tokens must still be from the configured issuer (Dex) and cryptographically valid.
	TrustedAudiences []string

	// AllowPrivateURLs permits OIDC discovery against Dex instances whose hostname
	// resolves to a private/internal IP address (e.g. dex.<mc>.<baseDomain> on a
	// private management cluster). When true, the HTTP client used for OIDC
	// discovery bypasses the built-in SSRF protection that normally blocks
	// connections to RFC-1918 ranges.
	//
	// Set MCP_OAUTH_ALLOW_PRIVATE_URLS=true only in trusted internal environments
	// where the Dex issuer URL uses internal DNS. TLS verification is still enforced.
	AllowPrivateURLs bool
}

// ConfigFromEnv builds a Config by reading the standard environment variables.
func ConfigFromEnv() Config {
	cfg := Config{
		Issuer:                  os.Getenv("MCP_OAUTH_ISSUER"),
		EncryptionKey:           os.Getenv("MCP_OAUTH_ENCRYPTION_KEY"),
		AllowPublicRegistration: os.Getenv("MCP_OAUTH_ALLOW_PUBLIC_REGISTRATION") == "true",
		StorageType:             os.Getenv("OAUTH_STORAGE"),
		ValkeyURL:               os.Getenv("VALKEY_URL"),
		ValkeyPassword:          os.Getenv("VALKEY_PASSWORD"),
		ValkeyTLS:               os.Getenv("VALKEY_TLS_ENABLED") == "true",
		ValkeyKeyPrefix:         os.Getenv("VALKEY_KEY_PREFIX"),
		DexIssuerURL:            os.Getenv("DEX_ISSUER_URL"),
		DexClientID:             os.Getenv("DEX_CLIENT_ID"),
		DexClientSecret:         os.Getenv("DEX_CLIENT_SECRET"),
		DexRedirectURL:          os.Getenv("DEX_REDIRECT_URL"),
		AllowPrivateURLs:        os.Getenv("MCP_OAUTH_ALLOW_PRIVATE_URLS") == "true",
	}
	if v := os.Getenv("OAUTH_TRUSTED_AUDIENCES"); v != "" {
		for _, a := range strings.Split(v, ",") {
			if a = strings.TrimSpace(a); a != "" {
				cfg.TrustedAudiences = append(cfg.TrustedAudiences, a)
			}
		}
	}
	return cfg
}

// NewHandler initialises the mcp-oauth Handler from the given Config.
// It returns the handler and a cleanup function that must be called on shutdown
// to flush and close the storage backend.
func NewHandler(ctx context.Context, cfg Config, logger *slog.Logger) (*mcpoauth.Handler, func(), error) {
	if cfg.Issuer == "" {
		return nil, nil, fmt.Errorf("oauth: MCP_OAUTH_ISSUER must be set")
	}
	if cfg.DexIssuerURL == "" || cfg.DexClientID == "" || cfg.DexClientSecret == "" {
		return nil, nil, fmt.Errorf("oauth: DEX_ISSUER_URL, DEX_CLIENT_ID, DEX_CLIENT_SECRET must all be set")
	}
	if cfg.DexRedirectURL == "" {
		return nil, nil, fmt.Errorf("oauth: DEX_REDIRECT_URL must be set")
	}

	dexCfg := &dex.Config{
		IssuerURL:    cfg.DexIssuerURL,
		ClientID:     cfg.DexClientID,
		ClientSecret: cfg.DexClientSecret,
		RedirectURL:  cfg.DexRedirectURL,
		// Request groups so the tenant resolver can match GrafanaOrganization RBAC.
		Scopes: []string{"openid", "profile", "email", "groups", "offline_access"},
	}
	if cfg.AllowPrivateURLs {
		// Inject an HTTP client that allows connections to private/internal IP
		// ranges. Required when the Dex issuer URL is an internal DNS name that
		// resolves to an RFC-1918 address (e.g. on a private management cluster).
		// TLS verification is still enforced by this client.
		dexCfg.HTTPClient = mcpoidc.NewPrivateIPAllowedHTTPClient(30 * time.Second)
	}
	provider, err := dex.NewProvider(dexCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("oauth: create Dex provider: %w", err)
	}

	return newHandlerWithProvider(ctx, provider, cfg, logger)
}

// newHandlerWithProvider wires a pre-built provider into the mcp-oauth server.
// It is separated from NewHandler so that tests can inject a mock provider
// without requiring a live Dex instance.
func newHandlerWithProvider(ctx context.Context, provider providers.Provider, cfg Config, logger *slog.Logger) (*mcpoauth.Handler, func(), error) {
	store, cleanup, err := newStore(ctx, cfg, logger)
	if err != nil {
		return nil, nil, err
	}

	serverCfg := &mcpoauth.ServerConfig{
		Issuer:                        cfg.Issuer,
		AllowPublicClientRegistration: cfg.AllowPublicRegistration,
		AllowRefreshTokenRotation:     true,
		TrustedAudiences:              cfg.TrustedAudiences,
	}

	srv, err := mcpoauth.NewServer(provider, store, store, store, serverCfg, logger)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("oauth: create server: %w", err)
	}

	if cfg.EncryptionKey != "" {
		keyBytes, err := hex.DecodeString(cfg.EncryptionKey)
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("oauth: decode MCP_OAUTH_ENCRYPTION_KEY (must be hex): %w", err)
		}
		enc, err := security.NewEncryptor(keyBytes)
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("oauth: create encryptor: %w", err)
		}
		srv.SetEncryptor(enc)
	} else {
		logger.Warn("MCP_OAUTH_ENCRYPTION_KEY is not set — OAuth tokens will be stored unencrypted. " +
			"Set MCP_OAUTH_ENCRYPTION_KEY to a 64-hex-char AES-256 key for production use. " +
			"Generate one with: openssl rand -hex 32")
	}

	handler := mcpoauth.NewHandler(srv, logger)
	return handler, cleanup, nil
}

// combinedStore is the set of store interfaces that a single backing store must
// implement to be usable by the mcp-oauth server.
type combinedStore interface {
	storage.TokenStore
	storage.ClientStore
	storage.FlowStore
}

// newStore creates the token storage backend based on cfg.StorageType.
func newStore(_ context.Context, cfg Config, _ *slog.Logger) (combinedStore, func(), error) {
	if cfg.StorageType == "valkey" {
		return newValkeyStore(cfg)
	}
	// Default: in-process memory store (dev / single-replica).
	s := memory.New()
	return s, s.Stop, nil
}

// newValkeyStore creates a production Valkey storage backend.
func newValkeyStore(cfg Config) (combinedStore, func(), error) {
	if cfg.ValkeyURL == "" {
		return nil, nil, fmt.Errorf("oauth: VALKEY_URL must be set when OAUTH_STORAGE=valkey")
	}

	vcfg := valkey.Config{
		Address:  cfg.ValkeyURL,
		Password: cfg.ValkeyPassword,
	}
	if cfg.ValkeyKeyPrefix != "" {
		vcfg.KeyPrefix = cfg.ValkeyKeyPrefix
	}
	if cfg.ValkeyTLS {
		vcfg.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	s, err := valkey.New(vcfg)
	if err != nil {
		return nil, nil, fmt.Errorf("oauth: connect to Valkey at %s: %w", cfg.ValkeyURL, err)
	}
	return s, func() { s.Close() }, nil
}
