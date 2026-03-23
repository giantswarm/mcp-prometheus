package prometheus

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/giantswarm/mcp-prometheus/internal/server"
)

// Note: discardLogger() is defined in tools_test.go (same package).

// queryResponse is a minimal valid Prometheus instant-query API response.
const queryResponse = `{"status":"success","data":{"resultType":"vector","result":[]}}`

// tlsQueryHandler handles /api/v1/query for TLS test servers.
func tlsQueryHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v1/query" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(queryResponse)) //nolint:errcheck
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// TestNewClientDefaultTransport verifies that a plain HTTP client is created
// when no TLS options are set.
func TestNewClientDefaultTransport(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(tlsQueryHandler))
	defer mockServer.Close()

	config := server.PrometheusConfig{URL: mockServer.URL}
	client := NewClient(config, discardLogger())
	if client.client == nil {
		t.Fatal("expected client to be initialized")
	}

	if _, err := client.ExecuteQuery(context.Background(), "up", ""); err != nil {
		t.Errorf("unexpected error with plain HTTP client: %v", err)
	}
}

// TestNewClientTLSSkipVerify verifies that a client with InsecureSkipVerify
// can connect to a TLS server whose certificate is not trusted.
func TestNewClientTLSSkipVerify(t *testing.T) {
	mockServer := httptest.NewTLSServer(http.HandlerFunc(tlsQueryHandler))
	defer mockServer.Close()

	config := server.PrometheusConfig{
		URL:           mockServer.URL,
		TLSSkipVerify: true,
	}
	client := NewClient(config, discardLogger())
	if client.client == nil {
		t.Fatal("expected client to be initialized with TLSSkipVerify")
	}

	if _, err := client.ExecuteQuery(context.Background(), "up", ""); err != nil {
		t.Errorf("unexpected error with TLSSkipVerify: %v", err)
	}
}

// TestNewClientCustomCA verifies that a client with a custom CA certificate
// can connect to a TLS server that uses that CA.
func TestNewClientCustomCA(t *testing.T) {
	mockServer := httptest.NewTLSServer(http.HandlerFunc(tlsQueryHandler))
	defer mockServer.Close()

	// Extract the server's leaf certificate and write it as PEM to a temp file.
	derBytes := mockServer.TLS.Certificates[0].Certificate[0]
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	tmpFile, err := os.CreateTemp(t.TempDir(), "ca-*.pem")
	if err != nil {
		t.Fatalf("failed to create temp CA file: %v", err)
	}
	if _, err := tmpFile.Write(certPEM); err != nil {
		t.Fatalf("failed to write CA cert: %v", err)
	}
	tmpFile.Close()

	config := server.PrometheusConfig{
		URL:       mockServer.URL,
		TLSCACert: tmpFile.Name(),
	}
	client := NewClient(config, discardLogger())
	if client.client == nil {
		t.Fatal("expected client to be initialized with custom CA")
	}

	if _, err := client.ExecuteQuery(context.Background(), "up", ""); err != nil {
		t.Errorf("unexpected error with custom CA: %v", err)
	}
}

// TestNewClientTLSCANotFound verifies that NewClient returns an uninitialised
// client (client.client == nil) when the CA file path does not exist.
func TestNewClientTLSCANotFound(t *testing.T) {
	config := server.PrometheusConfig{
		URL:       "https://localhost:9090",
		TLSCACert: "/nonexistent/path/ca.pem",
	}
	client := NewClient(config, discardLogger())
	if client.client != nil {
		t.Error("expected uninitialised client when CA file is missing")
	}
}

// TestNewClientTLSInvalidPEM verifies that NewClient returns an uninitialised
// client when the CA file exists but does not contain valid PEM data.
func TestNewClientTLSInvalidPEM(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "invalid-ca-*.pem")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpFile.WriteString("this is definitely not valid PEM content") //nolint:errcheck
	tmpFile.Close()

	config := server.PrometheusConfig{
		URL:       "https://localhost:9090",
		TLSCACert: tmpFile.Name(),
	}
	client := NewClient(config, discardLogger())
	if client.client != nil {
		t.Error("expected uninitialised client when CA cert is invalid PEM")
	}
}

// TestNewClientTLSSkipVerifyWithCustomCA verifies that setting both TLSSkipVerify
// and TLSCACert simultaneously is a valid combination: the custom CA is loaded
// (pool is set on the TLS config) and InsecureSkipVerify is also set.
func TestNewClientTLSSkipVerifyWithCustomCA(t *testing.T) {
	mockServer := httptest.NewTLSServer(http.HandlerFunc(tlsQueryHandler))
	defer mockServer.Close()

	// Write the server's cert as the CA so the pool is valid.
	derBytes := mockServer.TLS.Certificates[0].Certificate[0]
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	tmpFile, err := os.CreateTemp(t.TempDir(), "ca-*.pem")
	if err != nil {
		t.Fatalf("failed to create temp CA file: %v", err)
	}
	if _, err := tmpFile.Write(certPEM); err != nil {
		t.Fatalf("failed to write CA cert: %v", err)
	}
	tmpFile.Close()

	config := server.PrometheusConfig{
		URL:           mockServer.URL,
		TLSSkipVerify: true,
		TLSCACert:     tmpFile.Name(),
	}
	client := NewClient(config, discardLogger())
	if client.client == nil {
		t.Fatal("expected client to be initialized with TLSSkipVerify+CustomCA")
	}

	if _, err := client.ExecuteQuery(context.Background(), "up", ""); err != nil {
		t.Errorf("unexpected error with TLSSkipVerify+CustomCA: %v", err)
	}
}

// TestNewClientTLSUntrustedException verifies that a client WITHOUT TLS config
// fails to connect to a TLS server with a self-signed cert (i.e. the default
// transport correctly rejects it).
func TestNewClientTLSUntrustedException(t *testing.T) {
	mockServer := httptest.NewTLSServer(http.HandlerFunc(tlsQueryHandler))
	defer mockServer.Close()

	// No TLS config — default transport should reject the self-signed cert.
	config := server.PrometheusConfig{URL: mockServer.URL}
	client := NewClient(config, discardLogger())
	if client.client == nil {
		t.Fatal("expected client struct to be created (URL is non-empty)")
	}

	_, err := client.ExecuteQuery(context.Background(), "up", "")
	if err == nil {
		t.Error("expected TLS error when connecting without a trusted CA, got nil")
	}
}
