package oauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giantswarm/mcp-oauth/providers/mock"
	mcpserver "github.com/giantswarm/mcp-oauth/server"
)

const (
	testSecret        = "secret"
	testDexIssuer     = "https://dex.example.com"
	testMCPIssuer     = "https://mcp.example.com"
	testMusterIssuer  = "https://muster.example.com"
	testMusterJwksURL = "https://muster.example.com/.well-known/jwks.json"
	testTypAtJWT      = "at+jwt"
	testAlgRS256      = "RS256"
	testKidHeader     = "kid"
	testAlgHeader     = "alg"
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

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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

// --- OAUTH_TRUSTED_ISSUERS parsing ---

const testTrustedIssuersJSON = `[
  {
    "issuer": "https://muster.example.com",
    "jwksURL": "https://muster.example.com/.well-known/jwks.json",
    "allowedAudiences": ["https://mcp-prometheus.example.com"],
    "allowedScopes": ["prometheus:read"],
    "allowedClaims": {"sub": "*@giantswarm.io"},
    "acceptedTypHeaders": ["at+jwt"],
    "allowPrivateIPJWKS": true
  }
]`

func TestParseTrustedIssuersValid(t *testing.T) {
	issuers, err := parseTrustedIssuers(testTrustedIssuersJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issuers) != 1 {
		t.Fatalf("expected 1 issuer, got %d", len(issuers))
	}
	ti := issuers[0]
	if ti.Issuer != testMusterIssuer {
		t.Errorf("Issuer: got %q", ti.Issuer)
	}
	if ti.JwksURL != testMusterJwksURL {
		t.Errorf("JwksURL: got %q", ti.JwksURL)
	}
	if len(ti.AllowedAudiences) != 1 || ti.AllowedAudiences[0] != "https://mcp-prometheus.example.com" {
		t.Errorf("AllowedAudiences: got %v", ti.AllowedAudiences)
	}
	if len(ti.AllowedScopes) != 1 || ti.AllowedScopes[0] != "prometheus:read" {
		t.Errorf("AllowedScopes: got %v", ti.AllowedScopes)
	}
	if ti.AllowedClaims["sub"] != "*@giantswarm.io" {
		t.Errorf("AllowedClaims[sub]: got %q", ti.AllowedClaims["sub"])
	}
	if len(ti.AcceptedTypHeaders) != 1 || ti.AcceptedTypHeaders[0] != testTypAtJWT {
		t.Errorf("AcceptedTypHeaders: got %v", ti.AcceptedTypHeaders)
	}
	if !ti.AllowPrivateIPJWKS {
		t.Error("AllowPrivateIPJWKS: expected true")
	}
}

func TestParseTrustedIssuersPrivateIPJWKSHosts(t *testing.T) {
	raw := `[
  {
    "issuer": "https://muster.example.com",
    "jwksURL": "https://muster.example.com/.well-known/jwks.json",
    "allowPrivateIPJWKSHosts": ["muster.example.com"]
  }
]`
	issuers, err := parseTrustedIssuers(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issuers) != 1 {
		t.Fatalf("expected 1 issuer, got %d", len(issuers))
	}
	ti := issuers[0]
	if ti.AllowPrivateIPJWKS {
		t.Error("AllowPrivateIPJWKS: expected false")
	}
	if len(ti.AllowPrivateIPJWKSHosts) != 1 || ti.AllowPrivateIPJWKSHosts[0] != "muster.example.com" {
		t.Errorf("AllowPrivateIPJWKSHosts: got %v", ti.AllowPrivateIPJWKSHosts)
	}
}

func TestParseTrustedIssuersEmpty(t *testing.T) {
	issuers, err := parseTrustedIssuers("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issuers != nil {
		t.Errorf("expected nil issuers for empty input, got %v", issuers)
	}
}

func TestParseTrustedIssuersMalformed(t *testing.T) {
	if _, err := parseTrustedIssuers("{not json"); err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestParseTrustedIssuersMissingFields(t *testing.T) {
	cases := map[string]string{
		"missing issuer":  `[{"jwksURL": "https://muster.example.com/jwks"}]`,
		"missing jwksURL": `[{"issuer": "https://muster.example.com"}]`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseTrustedIssuers(raw); err == nil {
				t.Errorf("expected error for %s", name)
			}
		})
	}
}

func TestConfigFromEnvTrustedIssuers(t *testing.T) {
	t.Setenv("OAUTH_TRUSTED_ISSUERS", testTrustedIssuersJSON)

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.TrustedIssuers) != 1 {
		t.Fatalf("expected 1 trusted issuer, got %d", len(cfg.TrustedIssuers))
	}
	if cfg.TrustedIssuers[0].Issuer != testMusterIssuer {
		t.Errorf("Issuer: got %q", cfg.TrustedIssuers[0].Issuer)
	}
}

func TestConfigFromEnvTrustedIssuersInvalid(t *testing.T) {
	t.Setenv("OAUTH_TRUSTED_ISSUERS", "{not json")

	if _, err := ConfigFromEnv(); err == nil {
		t.Error("expected error for malformed OAUTH_TRUSTED_ISSUERS")
	}
}

// signRS256 mints a signed JWT with the given header and claims maps.
func signRS256(t *testing.T, key *rsa.PrivateKey, header, claims map[string]any) string {
	t.Helper()
	encode := func(v map[string]any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal JWT part: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signingInput := encode(header) + "." + encode(claims)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign JWT: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestValidateTokenEnforcesTrustedIssuerAllowedClaims(t *testing.T) {
	const (
		keyID    = "test-key"
		audience = "https://mcp-prometheus.example.com"
	)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	jwks := map[string]any{
		"keys": []map[string]any{{
			"kty":         "RSA",
			testKidHeader: keyID,
			"use":         "sig",
			"alg":         testAlgRS256,
			"n":           base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e":           base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}},
	}
	jwksServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(jwks); err != nil {
			t.Errorf("encode JWKS: %v", err)
		}
	}))
	defer jwksServer.Close()

	raw := fmt.Sprintf(`[
  {
    "issuer": %q,
    "jwksURL": %q,
    "allowedAudiences": [%q],
    "allowedClaims": {"sub": "*@giantswarm.io"},
    "allowPrivateIPJWKS": true
  }
]`, testMusterIssuer, jwksServer.URL, audience)
	issuers, err := parseTrustedIssuers(raw)
	if err != nil {
		t.Fatalf("parseTrustedIssuers: %v", err)
	}
	// RootCAs is runtime trust material, not part of the env-var config shape.
	pool := x509.NewCertPool()
	pool.AddCert(jwksServer.Certificate())
	issuers[0].RootCAs = pool

	h, cleanup, err := newHandlerWithProvider(t.Context(), mock.NewProvider(), Config{
		Issuer:         testMCPIssuer,
		TrustedIssuers: issuers,
	}, nil, slog.Default())
	if err != nil {
		t.Fatalf("newHandlerWithProvider: %v", err)
	}
	defer cleanup()

	protected := h.ValidateToken(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	mintToken := func(sub string) string {
		return signRS256(t, key,
			map[string]any{testAlgHeader: testAlgRS256, "typ": "at+jwt", "kid": keyID},
			map[string]any{
				"iss": testMusterIssuer,
				"sub": sub,
				"aud": audience,
				"iat": time.Now().Unix(),
				"exp": time.Now().Add(time.Minute).Unix(),
			})
	}

	cases := []struct {
		name       string
		subject    string
		wantStatus int
	}{
		{"sub matching *@giantswarm.io is accepted", "alice@giantswarm.io", http.StatusOK},
		{"sub not matching *@giantswarm.io is rejected", "mallory@evil.example", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			req.Header.Set("Authorization", "Bearer "+mintToken(tc.subject))
			rr := httptest.NewRecorder()
			protected.ServeHTTP(rr, req)
			if rr.Code != tc.wantStatus {
				t.Errorf("sub %q: got status %d, want %d (body: %s)", tc.subject, rr.Code, tc.wantStatus, rr.Body.String())
			}
		})
	}
}

func TestNewHandlerWithProviderTrustedIssuers(t *testing.T) {
	p := mock.NewProvider()
	cfg := Config{
		Issuer: testMCPIssuer,
		TrustedIssuers: []mcpserver.TrustedIssuer{
			{
				Issuer:           testMusterIssuer,
				JwksURL:          testMusterJwksURL,
				AllowedAudiences: []string{testMCPIssuer},
			},
		},
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

// --- Dex CA file (private-CA installations) ---

func TestConfigFromEnvDexCAFile(t *testing.T) {
	t.Setenv("DEX_CA_FILE", "/etc/ssl/certs/dex-ca/ca.crt")

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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
	pem := pemEncodeCert(t, server.Certificate())
	if err := os.WriteFile(caFile, pem, 0o600); err != nil {
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

// TestNewHandlerUnreadableDexCAFile verifies that a configured but unreadable
// DEX_CA_FILE fails startup instead of silently falling back to the system pool.
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

// TestTrustedIssuerInheritsDexCAPool verifies that trusted issuers without an
// explicit per-issuer pool have their JWKS TLS verified against the Dex CA pool.
func TestTrustedIssuerInheritsDexCAPool(t *testing.T) {
	const keyID = "test-key"
	const audience = testMCPIssuer

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	jwks := map[string]any{
		"keys": []map[string]any{{
			"kty":         "RSA",
			testKidHeader: keyID,
			"use":         "sig",
			"alg":         testAlgRS256,
			"n":           base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e":           base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}},
	}
	jwksServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(jwks); err != nil {
			t.Errorf("encode JWKS: %v", err)
		}
	}))
	defer jwksServer.Close()

	issuers := []mcpserver.TrustedIssuer{{
		Issuer:             testMusterIssuer,
		JwksURL:            jwksServer.URL,
		AllowedAudiences:   []string{audience},
		AllowPrivateIPJWKS: true,
	}}

	pool := x509.NewCertPool()
	pool.AddCert(jwksServer.Certificate())

	h, cleanup, err := newHandlerWithProvider(t.Context(), mock.NewProvider(), Config{
		Issuer:         testMCPIssuer,
		TrustedIssuers: issuers,
	}, pool, slog.Default())
	if err != nil {
		t.Fatalf("newHandlerWithProvider: %v", err)
	}
	defer cleanup()

	token := signRS256(t, key,
		map[string]any{testAlgHeader: testAlgRS256, "typ": "at+jwt", "kid": keyID},
		map[string]any{
			"iss": testMusterIssuer,
			"sub": "alice@giantswarm.io",
			"aud": audience,
			"iat": time.Now().Unix(),
			"exp": time.Now().Add(time.Minute).Unix(),
		})

	protected := h.ValidateToken(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	protected.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 with issuer JWKS verified via Dex CA pool, got %d (%s)", rr.Code, rr.Body.String())
	}
}
