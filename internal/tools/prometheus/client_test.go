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
		_, _ = w.Write([]byte(queryResponse))
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
	client, err := NewClient(config, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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
	client, err := NewClient(config, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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
	_ = tmpFile.Close()

	config := server.PrometheusConfig{
		URL:       mockServer.URL,
		TLSCACert: tmpFile.Name(),
	}
	client, err := NewClient(config, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error with custom CA: %v", err)
	}

	if _, err := client.ExecuteQuery(context.Background(), "up", ""); err != nil {
		t.Errorf("unexpected error with custom CA: %v", err)
	}
}

// TestNewClientTLSCANotFound verifies that NewClient returns an error when
// the CA file path does not exist.
func TestNewClientTLSCANotFound(t *testing.T) {
	config := server.PrometheusConfig{
		URL:       "https://localhost:9090",
		TLSCACert: "/nonexistent/path/ca.pem",
	}
	_, err := NewClient(config, discardLogger())
	if err == nil {
		t.Error("expected error when CA file is missing, got nil")
	}
}

// TestNewClientTLSInvalidPEM verifies that NewClient returns an error when
// the CA file exists but does not contain valid PEM data.
func TestNewClientTLSInvalidPEM(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "invalid-ca-*.pem")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	_, _ = tmpFile.WriteString("this is definitely not valid PEM content")
	_ = tmpFile.Close()

	config := server.PrometheusConfig{
		URL:       "https://localhost:9090",
		TLSCACert: tmpFile.Name(),
	}
	_, err = NewClient(config, discardLogger())
	if err == nil {
		t.Error("expected error when CA cert is invalid PEM, got nil")
	}
}

// TestNewClientEmptyURL verifies that NewClient returns an error for an empty URL.
func TestNewClientEmptyURL(t *testing.T) {
	_, err := NewClient(server.PrometheusConfig{}, discardLogger())
	if err == nil {
		t.Error("expected error for empty URL, got nil")
	}
}

// TestNewClientTLSSkipVerifyWithCustomCA verifies that setting both TLSSkipVerify
// and TLSCACert simultaneously is a valid combination.
func TestNewClientTLSSkipVerifyWithCustomCA(t *testing.T) {
	mockServer := httptest.NewTLSServer(http.HandlerFunc(tlsQueryHandler))
	defer mockServer.Close()

	derBytes := mockServer.TLS.Certificates[0].Certificate[0]
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	tmpFile, err := os.CreateTemp(t.TempDir(), "ca-*.pem")
	if err != nil {
		t.Fatalf("failed to create temp CA file: %v", err)
	}
	if _, err := tmpFile.Write(certPEM); err != nil {
		t.Fatalf("failed to write CA cert: %v", err)
	}
	_ = tmpFile.Close()

	config := server.PrometheusConfig{
		URL:           mockServer.URL,
		TLSSkipVerify: true,
		TLSCACert:     tmpFile.Name(),
	}
	client, err := NewClient(config, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := client.ExecuteQuery(context.Background(), "up", ""); err != nil {
		t.Errorf("unexpected error with TLSSkipVerify+CustomCA: %v", err)
	}
}

// TestNewClientTLSUntrustedException verifies that a client WITHOUT TLS config
// fails to connect to a TLS server with a self-signed cert.
func TestNewClientTLSUntrustedException(t *testing.T) {
	mockServer := httptest.NewTLSServer(http.HandlerFunc(tlsQueryHandler))
	defer mockServer.Close()

	config := server.PrometheusConfig{URL: mockServer.URL}
	client, err := NewClient(config, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}

	_, err = client.ExecuteQuery(context.Background(), "up", "")
	if err == nil {
		t.Error("expected TLS error when connecting without a trusted CA, got nil")
	}
}
