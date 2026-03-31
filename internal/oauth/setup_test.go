package oauth

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/giantswarm/mcp-oauth/providers/mock"
)

func TestConfigFromEnvDefaults(t *testing.T) {
	// Unset any OAuth env vars to test defaults.
	for _, key := range []string{
		"MCP_OAUTH_ISSUER", "MCP_OAUTH_ENCRYPTION_KEY",
		"MCP_OAUTH_ALLOW_PUBLIC_REGISTRATION",
		"MCP_OAUTH_ALLOW_PRIVATE_URLS",
		"OAUTH_STORAGE", "VALKEY_URL", "VALKEY_PASSWORD",
		"VALKEY_TLS_ENABLED", "VALKEY_KEY_PREFIX",
		"DEX_ISSUER_URL", "DEX_CLIENT_ID", "DEX_CLIENT_SECRET", "DEX_REDIRECT_URL",
	} {
		os.Unsetenv(key)
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
	os.Setenv("MCP_OAUTH_ISSUER", "https://issuer.example.com")
	os.Setenv("MCP_OAUTH_ENCRYPTION_KEY", "deadbeef")
	os.Setenv("MCP_OAUTH_ALLOW_PUBLIC_REGISTRATION", "true")
	os.Setenv("OAUTH_STORAGE", "valkey")
	os.Setenv("VALKEY_URL", "valkey://localhost:6379")
	os.Setenv("VALKEY_PASSWORD", "secret")
	os.Setenv("VALKEY_TLS_ENABLED", "true")
	os.Setenv("VALKEY_KEY_PREFIX", "myapp:")
	os.Setenv("DEX_ISSUER_URL", "https://dex.example.com")
	os.Setenv("DEX_CLIENT_ID", "mcp-prometheus")
	os.Setenv("DEX_CLIENT_SECRET", "dexsecret")
	os.Setenv("DEX_REDIRECT_URL", "https://app.example.com/oauth/callback")
	defer func() {
		for _, key := range []string{
			"MCP_OAUTH_ISSUER", "MCP_OAUTH_ENCRYPTION_KEY",
			"MCP_OAUTH_ALLOW_PUBLIC_REGISTRATION",
			"OAUTH_STORAGE", "VALKEY_URL", "VALKEY_PASSWORD",
			"VALKEY_TLS_ENABLED", "VALKEY_KEY_PREFIX",
			"DEX_ISSUER_URL", "DEX_CLIENT_ID", "DEX_CLIENT_SECRET", "DEX_REDIRECT_URL",
		} {
			os.Unsetenv(key)
		}
	}()

	cfg := ConfigFromEnv()

	checks := []struct {
		name string
		got  string
		want string
	}{
		{"Issuer", cfg.Issuer, "https://issuer.example.com"},
		{"EncryptionKey", cfg.EncryptionKey, "deadbeef"},
		{"StorageType", cfg.StorageType, "valkey"},
		{"ValkeyURL", cfg.ValkeyURL, "valkey://localhost:6379"},
		{"ValkeyPassword", cfg.ValkeyPassword, "secret"},
		{"ValkeyKeyPrefix", cfg.ValkeyKeyPrefix, "myapp:"},
		{"DexIssuerURL", cfg.DexIssuerURL, "https://dex.example.com"},
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
	os.Setenv("MCP_OAUTH_ALLOW_PRIVATE_URLS", "true")
	defer os.Unsetenv("MCP_OAUTH_ALLOW_PRIVATE_URLS")

	cfg := ConfigFromEnv()
	if !cfg.AllowPrivateURLs {
		t.Error("expected AllowPrivateURLs == true when MCP_OAUTH_ALLOW_PRIVATE_URLS=true")
	}
}

// --- NewHandler validation errors (no Dex/network required) ---

func TestNewHandlerMissingIssuer(t *testing.T) {
	cfg := Config{
		DexIssuerURL:    "https://dex.example.com",
		DexClientID:     "id",
		DexClientSecret: "secret",
		DexRedirectURL:  "https://app.example.com/callback",
	}
	_, _, err := NewHandler(context.Background(), cfg, slog.Default())
	if err == nil {
		t.Error("expected error when Issuer is empty")
	}
}

func TestNewHandlerMissingDexIssuer(t *testing.T) {
	cfg := Config{
		Issuer:          "https://mcp.example.com",
		DexClientID:     "id",
		DexClientSecret: "secret",
		DexRedirectURL:  "https://app.example.com/callback",
	}
	_, _, err := NewHandler(context.Background(), cfg, slog.Default())
	if err == nil {
		t.Error("expected error when DexIssuerURL is empty")
	}
}

func TestNewHandlerMissingRedirectURL(t *testing.T) {
	cfg := Config{
		Issuer:          "https://mcp.example.com",
		DexIssuerURL:    "https://dex.example.com",
		DexClientID:     "id",
		DexClientSecret: "secret",
		// DexRedirectURL intentionally missing
	}
	_, _, err := NewHandler(context.Background(), cfg, slog.Default())
	if err == nil {
		t.Error("expected error when DexRedirectURL is empty")
	}
}

// --- newStore (package-internal) ---

func TestNewStoreMemory(t *testing.T) {
	store, cleanup, err := newStore(context.Background(), Config{StorageType: ""}, slog.Default())
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
	_, _, err := newStore(context.Background(), Config{StorageType: "valkey", ValkeyURL: ""}, slog.Default())
	if err == nil {
		t.Error("expected error when VALKEY_URL is empty with valkey storage type")
	}
}

func TestNewValkeyStoreMissingURL(t *testing.T) {
	_, _, err := newValkeyStore(Config{ValkeyURL: ""})
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
	})
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
	})
	_ = err
}

// --- newHandlerWithProvider (mock provider, no Dex required) ---

func TestNewHandlerWithProviderMemoryStore(t *testing.T) {
	p := mock.NewProvider()
	cfg := Config{
		Issuer:                  "https://mcp.example.com",
		AllowPublicRegistration: true,
	}
	h, cleanup, err := newHandlerWithProvider(context.Background(), p, cfg, slog.Default())
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
		Issuer:        "https://mcp.example.com",
		EncryptionKey: "0102030405", // 5 bytes, not 32
	}
	_, _, err := newHandlerWithProvider(context.Background(), p, cfg, slog.Default())
	if err == nil {
		t.Error("expected error for short (non-32-byte) encryption key")
	}
}
