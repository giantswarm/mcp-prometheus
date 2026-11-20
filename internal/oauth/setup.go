package oauth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	mcpoauth "github.com/giantswarm/mcp-oauth"
	"github.com/giantswarm/mcp-oauth/handler"
	"github.com/giantswarm/mcp-oauth/providers"
	"github.com/giantswarm/mcp-oauth/providers/dex"
	"github.com/giantswarm/mcp-oauth/providers/google"
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

	// EncryptionKey is a 32-byte key, base64- or hex-encoded, used for AES-256-GCM token encryption.
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

	// Provider selects the upstream identity provider: [ProviderDex] (default)
	// or [ProviderGoogle]. Read from MCP_OAUTH_PROVIDER.
	Provider string

	// RedirectURL is the callback URL registered at the identity provider,
	// e.g. "https://mcp.example.com/oauth/callback". Read from
	// OAUTH_REDIRECT_URL, falling back to the legacy DEX_REDIRECT_URL.
	RedirectURL string

	// GoogleClientID is the OAuth client ID of the Google Cloud OAuth client
	// (provider "google").
	GoogleClientID string

	// GoogleClientSecret is the OAuth client secret of the Google Cloud OAuth
	// client (provider "google").
	GoogleClientSecret string

	// DexIssuerURL is the Dex OIDC issuer, e.g. "https://dex.example.com".
	DexIssuerURL string

	// DexClientID is the OAuth client ID registered in Dex.
	DexClientID string

	// DexClientSecret is the OAuth client secret registered in Dex.
	DexClientSecret string

	// TrustedAudiences lists OAuth client IDs whose tokens are accepted for SSO.
	// When an upstream aggregator (like muster) forwards a user's ID token,
	// mcp-prometheus accepts it if the token's audience matches any entry here.
	// Tokens must still be from the configured provider's issuer (Dex or
	// Google) and cryptographically valid.
	TrustedAudiences []string

	// AllowPrivateIPJWKS is accepted for compatibility and has no effect: the
	// mcp-oauth server permits the configured provider's issuer host to
	// resolve to a private address on its own.
	AllowPrivateIPJWKS bool

	// AllowPrivateIPJWKSHosts lists the hosts, other than the issuer's own,
	// whose JWKS may resolve to an RFC 1918 or RFC 4193 address: a jwks_uri
	// the IdP's discovery document puts on another host. The issuer host needs
	// no entry. Loopback and link-local stay refused. Read from
	// MCP_OAUTH_ALLOW_PRIVATE_IP_JWKS_HOSTS (comma-separated).
	AllowPrivateIPJWKSHosts []string

	// DeprecatedPrivateURLsEnv names the deprecated variable
	// AllowPrivateIPJWKS was also read from, empty when none is set.
	DeprecatedPrivateURLsEnv string

	// DexCAFile is the path to a PEM-encoded CA certificate file used to verify
	// TLS for Dex and for JWKS endpoints. Required when Dex is served with a
	// certificate from a private/internal CA (e.g. on private management
	// clusters). The pool is passed explicitly to the Dex provider client and
	// the forwarded-ID-token JWKS validation — mcp-oauth does not read a CA
	// installed on http.DefaultTransport.
	// Empty means the system trust store alone is used. Dex only.
	DexCAFile string
}

// Supported values for [Config.Provider] / MCP_OAUTH_PROVIDER.
const (
	// ProviderDex uses a Dex OIDC issuer (DEX_ISSUER_URL, DEX_CLIENT_ID,
	// DEX_CLIENT_SECRET). Default.
	ProviderDex = "dex"

	// ProviderGoogle uses Google as the OAuth 2.0 / OIDC provider
	// (GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET).
	ProviderGoogle = "google"
)

// googleScopes are requested from Google. Google ID tokens carry no groups
// claim, so tenancy modes that need groups (grafana-organization, static
// group map) cannot resolve tenants for Google users; use tenancy mode
// "none" or a static all-users tenant list with this provider.
var googleScopes = []string{"openid", "email", "profile"}

// envTrue is the string value that enables a boolean env var.
const envTrue = "true"

// storageTypeValkey selects the Valkey-backed token store via OAUTH_STORAGE.
const storageTypeValkey = "valkey"

// ConfigFromEnv builds a Config by reading the standard environment variables.
func ConfigFromEnv() (Config, error) {
	allowPrivateIPJWKS, deprecatedPrivateURLsEnv, err := privateIPJWKSFromEnv()
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		DeprecatedPrivateURLsEnv: deprecatedPrivateURLsEnv,
		Issuer:                   os.Getenv("MCP_OAUTH_ISSUER"),
		EncryptionKey:            os.Getenv("MCP_OAUTH_ENCRYPTION_KEY"),
		AllowPublicRegistration:  os.Getenv("MCP_OAUTH_ALLOW_PUBLIC_REGISTRATION") == envTrue,
		StorageType:              os.Getenv("OAUTH_STORAGE"),
		ValkeyURL:                os.Getenv("VALKEY_URL"),
		ValkeyPassword:           os.Getenv("VALKEY_PASSWORD"),
		ValkeyTLS:                os.Getenv("VALKEY_TLS_ENABLED") == envTrue,
		ValkeyKeyPrefix:          os.Getenv("VALKEY_KEY_PREFIX"),
		Provider:                 strings.ToLower(strings.TrimSpace(os.Getenv("MCP_OAUTH_PROVIDER"))),
		RedirectURL:              os.Getenv("OAUTH_REDIRECT_URL"),
		GoogleClientID:           os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret:       os.Getenv("GOOGLE_CLIENT_SECRET"),
		DexIssuerURL:             os.Getenv("DEX_ISSUER_URL"),
		DexClientID:              os.Getenv("DEX_CLIENT_ID"),
		DexClientSecret:          os.Getenv("DEX_CLIENT_SECRET"),
		AllowPrivateIPJWKS:       allowPrivateIPJWKS,
		AllowPrivateIPJWKSHosts:  splitList(os.Getenv("MCP_OAUTH_ALLOW_PRIVATE_IP_JWKS_HOSTS")),
		TrustedAudiences:         splitList(os.Getenv("OAUTH_TRUSTED_AUDIENCES")),
		DexCAFile:                os.Getenv("DEX_CA_FILE"),
	}
	if cfg.Provider == "" {
		cfg.Provider = ProviderDex
	}
	if cfg.RedirectURL == "" {
		// Legacy name from the Dex-only days; still honoured so existing
		// deployments keep working.
		cfg.RedirectURL = os.Getenv("DEX_REDIRECT_URL")
	}
	return cfg, nil
}

// splitList returns the non-empty, space-trimmed entries of a comma-separated
// value, nil when there are none.
func splitList(value string) []string {
	var entries []string
	for entry := range strings.SplitSeq(value, ",") {
		if entry = strings.TrimSpace(entry); entry != "" {
			entries = append(entries, entry)
		}
	}
	return entries
}

// privateIPJWKSFromEnv reads MCP_OAUTH_ALLOW_PRIVATE_IP_JWKS and its
// deprecated aliases; any of them set to true permits private JWKS addresses.
// It returns the first deprecated alias set, empty when none is.
func privateIPJWKSFromEnv() (bool, string, error) {
	var allow bool
	var deprecated string
	for _, name := range []string{"MCP_OAUTH_ALLOW_PRIVATE_IP_JWKS", "MCP_OAUTH_ALLOW_PRIVATE_URLS", "OAUTH_ALLOW_PRIVATE_URLS"} {
		raw := os.Getenv(name)
		if raw == "" {
			continue
		}
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return false, "", fmt.Errorf("%s: expected a boolean: %w", name, err)
		}
		if name != "MCP_OAUTH_ALLOW_PRIVATE_IP_JWKS" && deprecated == "" {
			deprecated = name
		}
		allow = allow || value
	}
	return allow, deprecated, nil
}

// NewHandler initialises the mcp-oauth Handler from the given Config.
// It returns the handler and a cleanup function that must be called on shutdown
// to flush and close the storage backend.
func NewHandler(ctx context.Context, cfg Config, logger *slog.Logger) (*handler.Handler, func(), error) {
	if cfg.Issuer == "" {
		return nil, nil, fmt.Errorf("oauth: MCP_OAUTH_ISSUER must be set")
	}
	if cfg.RedirectURL == "" {
		return nil, nil, fmt.Errorf("oauth: OAUTH_REDIRECT_URL (or legacy DEX_REDIRECT_URL) must be set")
	}

	warnPrivateIPJWKSInert(cfg, logger)

	var (
		provider providers.Provider
		rootCAs  *x509.CertPool
		err      error
	)
	switch cfg.Provider {
	case "", ProviderDex:
		provider, rootCAs, err = newDexProvider(cfg, logger)
	case ProviderGoogle:
		provider, err = newGoogleProvider(cfg, logger)
	default:
		return nil, nil, fmt.Errorf("oauth: unsupported MCP_OAUTH_PROVIDER %q (supported: %s, %s)",
			cfg.Provider, ProviderDex, ProviderGoogle)
	}
	if err != nil {
		return nil, nil, err
	}

	return newHandlerWithProvider(ctx, provider, cfg, rootCAs, logger)
}

// warnPrivateIPJWKSInert warns when a private-address JWKS setting is set to
// true; it has no effect.
func warnPrivateIPJWKSInert(cfg Config, logger *slog.Logger) {
	if !cfg.AllowPrivateIPJWKS {
		return
	}
	logger.Warn("MCP_OAUTH_ALLOW_PRIVATE_IP_JWKS (Helm app.oauth.allowPrivateIPJWKS, aliases MCP_OAUTH_ALLOW_PRIVATE_URLS and OAUTH_ALLOW_PRIVATE_URLS) has no effect and will be removed; the JWKS of the configured issuer host may resolve to a private address without it; list any other JWKS host in MCP_OAUTH_ALLOW_PRIVATE_IP_JWKS_HOSTS (Helm app.oauth.allowPrivateIPJWKSHosts)")
}

// newDexProvider validates the DEX_* configuration and builds the Dex OIDC
// provider. It also returns the CA pool loaded from DEX_CA_FILE (nil = system
// trust store) so the caller can hand it to the JWKS validation path.
func newDexProvider(cfg Config, logger *slog.Logger) (providers.Provider, *x509.CertPool, error) {
	if cfg.DexIssuerURL == "" || cfg.DexClientID == "" || cfg.DexClientSecret == "" {
		return nil, nil, fmt.Errorf("oauth: DEX_ISSUER_URL, DEX_CLIENT_ID, DEX_CLIENT_SECRET must all be set for MCP_OAUTH_PROVIDER=%s", ProviderDex)
	}

	rootCAs, err := loadRootCAs(cfg.DexCAFile)
	if err != nil {
		return nil, nil, fmt.Errorf("oauth: %w", err)
	}
	if rootCAs != nil {
		logger.Info("Using custom CA for Dex and JWKS TLS verification", "caFile", cfg.DexCAFile)
	}
	if cfg.DeprecatedPrivateURLsEnv != "" {
		logger.Warn("DEPRECATED: " + cfg.DeprecatedPrivateURLsEnv + " is read as MCP_OAUTH_ALLOW_PRIVATE_IP_JWKS and will be refused in v0.14.0; remove it")
	}
	provider, err := dex.NewProvider(dexProviderConfig(cfg, rootCAs))
	if err != nil {
		return nil, nil, fmt.Errorf("oauth: create Dex provider: %w", err)
	}
	logger.Info("Using Dex OIDC provider", "issuer", cfg.DexIssuerURL)
	return provider, rootCAs, nil
}

// dexProviderConfig preserves mcp-oauth's proxy handling and host-pinned redirects.
func dexProviderConfig(cfg Config, rootCAs *x509.CertPool) *dex.Config {
	return &dex.Config{
		IssuerURL:    cfg.DexIssuerURL,
		ClientID:     cfg.DexClientID,
		ClientSecret: cfg.DexClientSecret,
		RedirectURL:  cfg.RedirectURL,
		// Request groups so the tenant resolver can match GrafanaOrganization RBAC.
		Scopes:  []string{"openid", "profile", "email", "groups", "offline_access"},
		RootCAs: rootCAs,
	}
}

// newGoogleProvider validates the GOOGLE_* configuration and builds the Google
// provider. Google's discovery, token and JWKS endpoints are public and served
// with publicly trusted certificates, so DEX_CA_FILE and
// DEX_CA_FILE does not apply and is ignored with a warning.
func newGoogleProvider(cfg Config, logger *slog.Logger) (providers.Provider, error) {
	if cfg.GoogleClientID == "" || cfg.GoogleClientSecret == "" {
		return nil, fmt.Errorf("oauth: GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET must be set for MCP_OAUTH_PROVIDER=%s", ProviderGoogle)
	}
	if cfg.DexCAFile != "" {
		logger.Warn("DEX_CA_FILE is ignored for the Google provider (Google endpoints use public CAs)", "caFile", cfg.DexCAFile)
	}

	provider, err := google.NewProvider(&google.Config{
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Scopes:       googleScopes,
	})
	if err != nil {
		return nil, fmt.Errorf("oauth: create Google provider: %w", err)
	}
	logger.Info("Using Google OAuth provider")
	return provider, nil
}

// loadRootCAs builds a certificate pool from the system pool plus the
// PEM-encoded CA certificate(s) in caFile. An empty caFile yields (nil, nil),
// selecting the system trust store everywhere the pool is consumed.
func loadRootCAs(caFile string) (*x509.CertPool, error) {
	if caFile == "" {
		return nil, nil
	}
	// #nosec G304 G703 -- caFile is the operator-configured CA path, never request input.
	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA file %s: %w", caFile, err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("parse CA certificate from %s", caFile)
	}
	return pool, nil
}

// newHandlerWithProvider wires a pre-built provider into the mcp-oauth server.
// It is separated from NewHandler so that tests can inject a mock provider
// without requiring a live Dex instance. rootCAs verifies JWKS endpoint TLS
// for forwarded-ID-token validation; nil = system pool.
func newHandlerWithProvider(ctx context.Context, provider providers.Provider, cfg Config, rootCAs *x509.CertPool, logger *slog.Logger) (*handler.Handler, func(), error) {
	enc, err := buildEncryptor(cfg, logger)
	if err != nil {
		return nil, nil, err
	}

	store, cleanup, err := newStore(ctx, cfg, logger, enc)
	if err != nil {
		return nil, nil, err
	}

	srv, err := mcpoauth.NewServer(provider, store, store, store, serverConfig(cfg, rootCAs), logger)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("oauth: create server: %w", err)
	}

	return handler.New(srv, logger), cleanup, nil
}

// serverConfig builds the mcp-oauth server configuration from cfg.
func serverConfig(cfg Config, rootCAs *x509.CertPool) *mcpoauth.ServerConfig {
	return &mcpoauth.ServerConfig{
		EnableClientIDMetadataDocuments: true,
		Issuer:                          cfg.Issuer,
		AllowPublicClientRegistration:   cfg.AllowPublicRegistration,
		AllowRefreshTokenRotation:       true,
		TrustedAudiences:                cfg.TrustedAudiences,
		AllowPrivateIPJWKSHosts:         cfg.AllowPrivateIPJWKSHosts,
		JWKSRootCAs:                     rootCAs,
	}
}

// encryptionKeyFormat is the MCP_OAUTH_ENCRYPTION_KEY format buildEncryptor
// accepts, quoted in every message about the key.
const encryptionKeyFormat = "32 random bytes, base64-encoded (openssl rand -base64 32) or hex-encoded (openssl rand -hex 32)"

// buildEncryptor returns the storage-layer Encryptor configured by cfg.
// Returns (nil, nil) when MCP_OAUTH_ENCRYPTION_KEY is unset; the store is
// then constructed without encryption.
//
// security.DecodeKey tries base64 first, then hex: a 64-character hex key is
// valid base64 too, but decodes to 48 bytes there and so falls through to
// hex. muster decodes its key the same way.
func buildEncryptor(cfg Config, logger *slog.Logger) (*security.Encryptor, error) {
	if cfg.EncryptionKey == "" {
		logger.Warn("MCP_OAUTH_ENCRYPTION_KEY is not set — OAuth tokens will be stored unencrypted. " +
			"Set it for production use to " + encryptionKeyFormat + ".")
		return nil, nil
	}
	keyBytes, err := security.DecodeKey(cfg.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("oauth: decode MCP_OAUTH_ENCRYPTION_KEY, which must be %s: %w", encryptionKeyFormat, err)
	}
	enc, err := security.NewEncryptor(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("oauth: create encryptor: %w", err)
	}
	return enc, nil
}

// combinedStore is the set of store interfaces that a single backing store must
// implement to be usable by the mcp-oauth server.
type combinedStore interface {
	storage.TokenStore
	storage.ClientStore
	storage.FlowStore
}

// newStore creates the token storage backend based on cfg.StorageType.
// When enc is non-nil, the backend encrypts tokens at rest.
func newStore(_ context.Context, cfg Config, _ *slog.Logger, enc *security.Encryptor) (combinedStore, func(), error) {
	if cfg.StorageType == storageTypeValkey {
		return newValkeyStore(cfg, enc)
	}
	// Default: in-process memory store (dev / single-replica).
	var opts []memory.Option
	if enc != nil {
		opts = append(opts, memory.WithEncryptor(enc))
	}
	s := memory.New(opts...)
	return s, s.Stop, nil
}

// newValkeyStore creates a production Valkey storage backend.
// When enc is non-nil, tokens are encrypted at rest with AES-256-GCM.
func newValkeyStore(cfg Config, enc *security.Encryptor) (combinedStore, func(), error) {
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

	var opts []valkey.Option
	if enc != nil {
		opts = append(opts, valkey.WithEncryptor(enc))
	}
	s, err := valkey.New(vcfg, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("oauth: connect to Valkey at %s: %w", cfg.ValkeyURL, err)
	}
	return s, func() { s.Close() }, nil
}
