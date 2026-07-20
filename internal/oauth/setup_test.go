package oauth

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/giantswarm/mcp-oauth/providers/mock"
)

const (
	testSecret    = "secret"
	testDexIssuer = "https://dex.example.com"
	testMCPIssuer = "https://mcp.example.com"
)

func TestConfigFromEnvDefaults(t *testing.T) {
	// Clear any OAuth env vars to test defaults. ConfigFromEnv uses os.Getenv,
	// which returns "" whether the var is unset or empty, so t.Setenv(key, "")
	// is equivalent and auto-restores at test end.
	for _, key := range []string{
		"MCP_OAUTH_ISSUER", "MCP_OAUTH_ENCRYPTION_KEY",
		"MCP_OAUTH_ALLOW_PUBLIC_REGISTRATION",
		"MCP_OAUTH_ALLOW_PRIVATE_URLS",
		"OAUTH_STORAGE", "VALKEY_URL", "VALKEY_PASSWORD",
		"VALKEY_TLS_ENABLED", "VALKEY_KEY_PREFIX",
		"DEX_ISSUER_URL", "DEX_CLIENT_ID", "DEX_CLIENT_SECRET", "DEX_REDIRECT_URL",
	} {
		t.Setenv(key, "")
	}

	cfg := ConfigFromEnv()

	if cfg.StorageType != "" {
		t.Errorf("expected empty StorageType by default, got %q", cfg.StorageType)
	}
	if cfg.AllowPublicRegistration {
		t.Error("expected AllowPublicRegistration == false by default")
	}
	if cfg.ValkeyTLS {
		t.Error("expected ValkeyTLS == false by default")
	}
	if cfg.AllowPrivateURLs {
		t.Error("expected AllowPrivateURLs == false by default")
	}
}

func TestConfigFromEnvReadsValues(t *testing.T) {
	t.Setenv("MCP_OAUTH_ISSUER", "https://issuer.example.com")
	t.Setenv("MCP_OAUTH_ENCRYPTION_KEY", "deadbeef")
	t.Setenv("MCP_OAUTH_ALLOW_PUBLIC_REGISTRATION", "true")
	t.Setenv("OAUTH_STORAGE", storageTypeValkey)
	t.Setenv("VALKEY_URL", "valkey://localhost:6379")
	t.Setenv("VALKEY_PASSWORD", testSecret)
	t.Setenv("VALKEY_TLS_ENABLED", "true")
	t.Setenv("VALKEY_KEY_PREFIX", "myapp:")
	t.Setenv("DEX_ISSUER_URL", testDexIssuer)
	t.Setenv("DEX_CLIENT_ID", "mcp-prometheus")
	t.Setenv("DEX_CLIENT_SECRET", "dexsecret")
	t.Setenv("DEX_REDIRECT_URL", "https://app.example.com/oauth/callback")

	cfg := ConfigFromEnv()

	checks := []struct {
		name string
		got  string
		want string
	}{
		{"Issuer", cfg.Issuer, "https://issuer.example.com"},
		{"EncryptionKey", cfg.EncryptionKey, "deadbeef"},
		{"StorageType", cfg.StorageType, storageTypeValkey},
		{"ValkeyURL", cfg.ValkeyURL, "valkey://localhost:6379"},
		{"ValkeyPassword", cfg.ValkeyPassword, testSecret},
		{"ValkeyKeyPrefix", cfg.ValkeyKeyPrefix, "myapp:"},
		{"DexIssuerURL", cfg.DexIssuerURL, testDexIssuer},
		{"DexClientID", cfg.DexClientID, "mcp-prometheus"},
		{"DexClientSecret", cfg.DexClientSecret, "dexsecret"},
		{"DexRedirectURL", cfg.DexRedirectURL, "https://app.example.com/oauth/callback"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}
	if !cfg.AllowPublicRegistration {
		t.Error("expected AllowPublicRegistration == true")
	}
	if !cfg.ValkeyTLS {
		t.Error("expected ValkeyTLS == true")
	}
}

func TestConfigFromEnvAllowPrivateURLs(t *testing.T) {
	t.Setenv("MCP_OAUTH_ALLOW_PRIVATE_URLS", "true")

	cfg := ConfigFromEnv()
	if !cfg.AllowPrivateURLs {
		t.Error("expected AllowPrivateURLs == true when MCP_OAUTH_ALLOW_PRIVATE_URLS=true")
	}
}

// --- NewHandler validation errors (no Dex/network required) ---

func TestNewHandlerMissingIssuer(t *testing.T) {
	cfg := Config{
		DexIssuerURL:    testDexIssuer,
		DexClientID:     "id",
		DexClientSecret: testSecret,
		DexRedirectURL:  "https://app.example.com/callback",
	}
	_, _, err := NewHandler(context.Background(), cfg, slog.Default())
	if err == nil {
		t.Error("expected error when Issuer is empty")
	}
}

func TestNewHandlerMissingDexIssuer(t *testing.T) {
	cfg := Config{
		Issuer:          testMCPIssuer,
		DexClientID:     "id",
		DexClientSecret: testSecret,
		DexRedirectURL:  "https://app.example.com/callback",
	}
	_, _, err := NewHandler(context.Background(), cfg, slog.Default())
	if err == nil {
		t.Error("expected error when DexIssuerURL is empty")
	}
}

func TestNewHandlerMissingRedirectURL(t *testing.T) {
	cfg := Config{
		Issuer:          testMCPIssuer,
		DexIssuerURL:    testDexIssuer,
		DexClientID:     "id",
		DexClientSecret: testSecret,
		// DexRedirectURL intentionally missing
	}
	_, _, err := NewHandler(context.Background(), cfg, slog.Default())
	if err == nil {
		t.Error("expected error when DexRedirectURL is empty")
	}
}

// --- newStore (package-internal) ---

func TestNewStoreMemory(t *testing.T) {
	store, cleanup, err := newStore(context.Background(), Config{StorageType: ""}, slog.Default(), nil)
	if err != nil {
		t.Fatalf("unexpected error creating memory store: %v", err)
	}
	if store == nil {
		t.Error("expected non-nil store")
	}
	if cleanup == nil {
		t.Error("expected non-nil cleanup function")
	}
	cleanup()
}

func TestNewStoreValkeyMissingURL(t *testing.T) {
	_, _, err := newStore(context.Background(), Config{StorageType: storageTypeValkey, ValkeyURL: ""}, slog.Default(), nil)
	if err == nil {
		t.Error("expected error when VALKEY_URL is empty with valkey storage type")
	}
}

func TestNewValkeyStoreMissingURL(t *testing.T) {
	_, _, err := newValkeyStore(Config{ValkeyURL: ""}, nil)
	if err == nil {
		t.Error("expected error for empty ValkeyURL")
	}
}

func TestNewValkeyStoreTLSBranch(t *testing.T) {
	// With a non-empty but unreachable URL, valkey.New may fail at connect
	// time — which is acceptable. The test verifies the TLS branch is
	// reached without panicking.
	_, _, err := newValkeyStore(Config{
		ValkeyURL: "127.0.0.1:1",
		ValkeyTLS: true,
	}, nil)
	// Any outcome (success or error) is fine; we just guard against panics.
	_ = err
}

func TestNewValkeyStoreKeyPrefixBranch(t *testing.T) {
	// Exercises the vcfg.KeyPrefix assignment branch. valkey.New will fail
	// because 127.0.0.1:1 is unreachable, which is fine — we only need the
	// key-prefix branch to be reached without panicking.
	_, _, err := newValkeyStore(Config{
		ValkeyURL:       "127.0.0.1:1",
		ValkeyKeyPrefix: "test:",
	}, nil)
	_ = err
}

// --- newHandlerWithProvider (mock provider, no Dex required) ---

func TestNewHandlerWithProviderMemoryStore(t *testing.T) {
	p := mock.NewProvider()
	cfg := Config{
		Issuer:                  testMCPIssuer,
		AllowPublicRegistration: true,
	}
	h, cleanup, err := newHandlerWithProvider(context.Background(), p, cfg, nil, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanup()
	if h == nil {
		t.Error("expected non-nil handler")
	}
}

func TestNewHandlerWithProviderShortEncryptionKey(t *testing.T) {
	// A hex string that decodes to fewer than 32 bytes should fail at
	// security.NewEncryptor (AES-256 requires exactly 32 bytes).
	p := mock.NewProvider()
	cfg := Config{
		Issuer:        testMCPIssuer,
		EncryptionKey: "0102030405", // 5 bytes, not 32
	}
	_, _, err := newHandlerWithProvider(context.Background(), p, cfg, nil, slog.Default())
	if err == nil {
		t.Error("expected error for short (non-32-byte) encryption key")
	}
}

// --- Dex CA file (private-CA installations) ---

func TestConfigFromEnvDexCAFile(t *testing.T) {
	t.Setenv("DEX_CA_FILE", "/etc/ssl/certs/dex-ca/ca.crt")

	cfg := ConfigFromEnv()
	if cfg.DexCAFile != "/etc/ssl/certs/dex-ca/ca.crt" {
		t.Errorf("DexCAFile: got %q", cfg.DexCAFile)
	}
}

func TestLoadRootCAsEmptyPath(t *testing.T) {
	pool, err := loadRootCAs("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool != nil {
		t.Error("expected nil pool for empty path (system trust store)")
	}
}

func TestLoadRootCAsMissingFile(t *testing.T) {
	if _, err := loadRootCAs(filepath.Join(t.TempDir(), "absent.crt")); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadRootCAsInvalidPEM(t *testing.T) {
	caFile := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(caFile, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRootCAs(caFile); err == nil {
		t.Error("expected error for invalid PEM")
	}
}

func TestLoadRootCAsValid(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	caFile := filepath.Join(t.TempDir(), "ca.crt")
	pemBytes := pemEncodeCert(t, server.Certificate())
	if err := os.WriteFile(caFile, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	pool, err := loadRootCAs(caFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}

	resp, err := httpClientWithRootCAs(pool).Get(server.URL)
	if err != nil {
		t.Fatalf("request against pool-verified server: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
}

func pemEncodeCert(t *testing.T, cert *x509.Certificate) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}

func TestNewHandlerUnreadableDexCAFile(t *testing.T) {
	cfg := Config{
		Issuer:          testMCPIssuer,
		DexIssuerURL:    testDexIssuer,
		DexClientID:     "mcp-prometheus",
		DexClientSecret: testSecret,
		DexRedirectURL:  testMCPIssuer + "/oauth/callback",
		DexCAFile:       filepath.Join(t.TempDir(), "absent.crt"),
	}
	if _, _, err := NewHandler(t.Context(), cfg, slog.Default()); err == nil {
		t.Error("expected error for unreadable DEX_CA_FILE")
	}
}
