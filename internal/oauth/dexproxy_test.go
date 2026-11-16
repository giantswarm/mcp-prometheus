package oauth

import (
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/giantswarm/mcp-oauth/providers/dex"
)

const (
	dexProxyChildEnv = "MCP_PROMETHEUS_DEX_PROXY_CHILD"
	dexProxyIssuer   = "https://example.com"
)

func TestDexProviderConfigLeavesClientToMCPOAuth(t *testing.T) {
	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()
	caFile := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(caFile, pemEncodeCert(t, server.Certificate()), 0o600); err != nil {
		t.Fatal(err)
	}
	rootCAs, err := loadRootCAs(caFile)
	if err != nil {
		t.Fatal(err)
	}

	for _, allowPrivateURLs := range []bool{false, true} {
		dexConfig := dexProviderConfig(Config{
			DexIssuerURL:     dexProxyIssuer,
			DexClientID:      "client",
			DexClientSecret:  "secret",
			RedirectURL:      "https://mcp-prometheus.example.com/oauth/callback",
			AllowPrivateURLs: allowPrivateURLs,
		}, rootCAs)
		if dexConfig.HTTPClient != nil {
			t.Errorf("AllowPrivateURLs=%t: HTTPClient is set, want mcp-oauth's default Dex client", allowPrivateURLs)
		}
		if dexConfig.AllowPrivateIP {
			t.Errorf("AllowPrivateURLs=%t: AllowPrivateIP is set, want false", allowPrivateURLs)
		}
		if dexConfig.RootCAs != rootCAs {
			t.Errorf("AllowPrivateURLs=%t: RootCAs is not the DEX_CA_FILE pool", allowPrivateURLs)
		}
	}
}

func TestDexDiscoveryFollowsProxyEnvironment(t *testing.T) {
	dexServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 dexProxyIssuer,
			"authorization_endpoint": dexProxyIssuer + "/auth",
			"token_endpoint":         dexProxyIssuer + "/token",
			"jwks_uri":               dexProxyIssuer + "/keys",
		})
	}))
	defer dexServer.Close()
	caPEM := string(pemEncodeCert(t, dexServer.Certificate()))

	tests := []struct {
		name          string
		noProxy       string
		wantTunnel    bool
		wantDiscovery bool
	}{
		{name: "issuer behind HTTPS_PROXY", wantTunnel: true, wantDiscovery: true},
		{name: "issuer in NO_PROXY", noProxy: "example.com", wantTunnel: false, wantDiscovery: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			proxy := newTunnelProxy(t, dexServer.Listener.Addr().String())

			command := exec.CommandContext(t.Context(), os.Args[0], //nolint:gosec // re-executes this test binary
				"-test.run=^TestDexDiscoveryProxyChild$", "-test.count=1", "-test.v")
			command.Env = append(environmentWithoutProxy(),
				dexProxyChildEnv+"=1",
				"DEX_PROXY_TEST_CA_PEM="+caPEM,
				"HTTPS_PROXY="+proxy.URL(),
				"NO_PROXY="+tc.noProxy,
			)
			output, _ := command.CombinedOutput()

			tunnels := proxy.connectedHosts()
			if tc.wantTunnel && (len(tunnels) == 0 || tunnels[0] != "example.com:443") {
				t.Errorf("proxy CONNECT hosts = %v, want example.com:443\nchild output:\n%s", tunnels, output)
			}
			if !tc.wantTunnel && len(tunnels) != 0 {
				t.Errorf("proxy CONNECT hosts = %v, want none for a NO_PROXY issuer\nchild output:\n%s", tunnels, output)
			}
			discovered := strings.Contains(string(output), "DEX-DISCOVERY-OK")
			if discovered != tc.wantDiscovery {
				t.Errorf("discovery succeeded = %t, want %t\nchild output:\n%s", discovered, tc.wantDiscovery, output)
			}
		})
	}
}

// TestDexDiscoveryProxyChild runs only as the child process of
// TestDexDiscoveryFollowsProxyEnvironment: net/http reads the proxy variables
// once per process, so each proxy setting needs a fresh process.
func TestDexDiscoveryProxyChild(t *testing.T) {
	if os.Getenv(dexProxyChildEnv) != "1" {
		t.Skip("child process of TestDexDiscoveryFollowsProxyEnvironment")
	}
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM([]byte(os.Getenv("DEX_PROXY_TEST_CA_PEM"))) {
		t.Fatal("DEX_PROXY_TEST_CA_PEM holds no certificate")
	}
	dexConfig := dexProviderConfig(Config{
		DexIssuerURL:     dexProxyIssuer,
		DexClientID:      "client",
		DexClientSecret:  "secret",
		RedirectURL:      "https://mcp-prometheus.example.com/oauth/callback",
		AllowPrivateURLs: true,
	}, rootCAs)
	dexConfig.DiscoveryTimeout = 2 * time.Second
	if _, err := dex.NewProvider(dexConfig); err != nil {
		t.Logf("DEX-DISCOVERY-FAILED: %v", err)
		return
	}
	t.Log("DEX-DISCOVERY-OK")
}

func environmentWithoutProxy() []string {
	var environment []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasSuffix(strings.ToUpper(name), "_PROXY") {
			continue
		}
		environment = append(environment, entry)
	}
	return environment
}

// tunnelProxy is an HTTPS proxy that records each CONNECT target and tunnels
// every one of them to a fixed backend address.
type tunnelProxy struct {
	server  *httptest.Server
	mutex   sync.Mutex
	targets []string
}

func newTunnelProxy(t *testing.T, backendAddress string) *tunnelProxy {
	t.Helper()
	proxy := &tunnelProxy{}
	proxy.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT only", http.StatusMethodNotAllowed)
			return
		}
		proxy.mutex.Lock()
		proxy.targets = append(proxy.targets, r.Host)
		proxy.mutex.Unlock()

		backend, err := net.Dial("tcp", backendAddress)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			_ = backend.Close()
			http.Error(w, "hijacking unsupported", http.StatusInternalServerError)
			return
		}
		client, _, err := hijacker.Hijack()
		if err != nil {
			_ = backend.Close()
			return
		}
		_, _ = client.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
		go func() {
			_, _ = io.Copy(backend, client)
			_ = backend.Close()
		}()
		_, _ = io.Copy(client, backend)
		_ = client.Close()
	}))
	t.Cleanup(proxy.server.Close)
	return proxy
}

func (proxy *tunnelProxy) URL() string { return proxy.server.URL }

func (proxy *tunnelProxy) connectedHosts() []string {
	proxy.mutex.Lock()
	defer proxy.mutex.Unlock()
	return append([]string(nil), proxy.targets...)
}
