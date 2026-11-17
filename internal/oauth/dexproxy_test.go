package oauth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"

	"github.com/giantswarm/mcp-oauth/providers/dex"
)

const dexProxyChildEnv = "MCP_PROMETHEUS_DEX_PROXY_CHILD"

const dexProxyHostname = "dex-proxy.test"

func TestDexDiscoveryFollowsProxyEnvironment(t *testing.T) {
	var issuer string
	var discoveries atomic.Int32
	backend := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(writer, request)
			return
		}
		discoveries.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(map[string]string{
			"issuer":                 issuer,
			"authorization_endpoint": issuer + "/auth",
			"token_endpoint":         issuer + "/token",
			"jwks_uri":               issuer + "/keys",
		}); err != nil {
			t.Errorf("encode discovery document: %v", err)
		}
	}))
	certificate, certificatePEM := dexProxyCertificate(t)
	backend.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	backend.StartTLS()
	t.Cleanup(backend.Close)
	_, port, err := net.SplitHostPort(backend.Listener.Addr().String())
	require.NoError(t, err)
	issuer = "https://" + net.JoinHostPort(dexProxyHostname, port)
	caFile := filepath.Join(t.TempDir(), "ca.crt")
	require.NoError(t, os.WriteFile(caFile, certificatePEM, 0o600))

	for _, testCase := range []struct {
		name       string
		noProxy    string
		wantTunnel bool
	}{
		{name: "issuer behind HTTPS_PROXY", wantTunnel: true},
		{name: "issuer in NO_PROXY", noProxy: dexProxyHostname},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			proxy := newTunnelProxy(t, backend.Listener.Addr().String())
			before := discoveries.Load()
			childContext, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(childContext, os.Args[0], //nolint:gosec // Executes the current test binary.
				"-test.run=^TestDexDiscoveryProxyChild$", "-test.count=1")
			command.Env = append(dexProxyEnvironment(),
				dexProxyChildEnv+"=1",
				"DEX_PROXY_TEST_ISSUER="+issuer,
				"SSL_CERT_FILE="+caFile,
				"SSL_CERT_DIR="+t.TempDir(),
				"HTTPS_PROXY="+proxy.server.URL,
				"NO_PROXY="+testCase.noProxy,
			)
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			require.Greater(t, discoveries.Load(), before, "discovery must reach the local issuer")
			tunnels := proxy.connectedHosts()
			if testCase.wantTunnel {
				require.Equal(t, []string{net.JoinHostPort(dexProxyHostname, port)}, tunnels)
			} else {
				require.Empty(t, tunnels)
			}
		})
	}
}

func dexProxyCertificate(t *testing.T) (tls.Certificate, []byte) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		DNSNames:     []string{dexProxyHostname},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{certificateDER}, PrivateKey: privateKey},
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
}

// Each child uses a fresh resolver and proxy environment, without replacing the HTTP transport.
func installDexProxyResolver(t *testing.T) {
	t.Helper()
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			client, server := net.Pipe()
			go func() {
				defer func() { _ = server.Close() }()
				var length [2]byte
				if _, err := io.ReadFull(server, length[:]); err != nil {
					return
				}
				queryBytes := make([]byte, binary.BigEndian.Uint16(length[:]))
				if _, err := io.ReadFull(server, queryBytes); err != nil {
					return
				}
				var query dnsmessage.Message
				if err := query.Unpack(queryBytes); err != nil {
					return
				}
				response := dnsmessage.Message{
					Header:    dnsmessage.Header{ID: query.ID, Response: true, Authoritative: true, RecursionAvailable: true},
					Questions: query.Questions,
				}
				for _, question := range query.Questions {
					if question.Name.String() == dexProxyHostname+"." && question.Type == dnsmessage.TypeA {
						response.Answers = append(response.Answers, dnsmessage.Resource{
							Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET},
							Body:   &dnsmessage.AResource{A: [4]byte{127, 0, 0, 1}},
						})
					}
				}
				responseBytes, err := response.Pack()
				if err != nil || len(responseBytes) > 65535 {
					return
				}
				binary.BigEndian.PutUint16(length[:], uint16(len(responseBytes))) //nolint:gosec // DNS responses are bounded to 65535 bytes.
				_, _ = server.Write(append(length[:], responseBytes...))
			}()
			return client, nil
		},
	}
}

func dexProxyEnvironment() []string {
	var environment []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasSuffix(strings.ToUpper(name), "_PROXY") ||
			strings.HasPrefix(name, "OAUTH_") || strings.HasPrefix(name, "MCP_OAUTH_") ||
			strings.HasPrefix(name, "SSL_CERT_") {
			continue
		}
		environment = append(environment, entry)
	}
	return environment
}

type tunnelProxy struct {
	server  *httptest.Server
	mutex   sync.Mutex
	targets []string
}

func newTunnelProxy(t *testing.T, backendAddress string) *tunnelProxy {
	t.Helper()
	proxy := &tunnelProxy{}
	proxy.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodConnect {
			http.Error(writer, "CONNECT only", http.StatusMethodNotAllowed)
			return
		}
		proxy.mutex.Lock()
		proxy.targets = append(proxy.targets, request.Host)
		proxy.mutex.Unlock()
		backendConnection, err := net.DialTimeout("tcp", backendAddress, time.Second)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadGateway)
			return
		}
		hijacker, ok := writer.(http.Hijacker)
		if !ok {
			_ = backendConnection.Close()
			http.Error(writer, "hijacking unsupported", http.StatusInternalServerError)
			return
		}
		clientConnection, _, err := hijacker.Hijack()
		if err != nil {
			_ = backendConnection.Close()
			return
		}
		_, _ = clientConnection.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
		go func() {
			_, _ = io.Copy(backendConnection, clientConnection)
			_ = backendConnection.Close()
		}()
		_, _ = io.Copy(clientConnection, backendConnection)
		_ = clientConnection.Close()
	}))
	t.Cleanup(proxy.server.Close)
	return proxy
}

func (proxy *tunnelProxy) connectedHosts() []string {
	proxy.mutex.Lock()
	defer proxy.mutex.Unlock()
	return append([]string(nil), proxy.targets...)
}

func TestDexDiscoveryProxyChild(t *testing.T) {
	if os.Getenv(dexProxyChildEnv) != "1" {
		t.Skip("requires the parent proxy fixture")
	}
	installDexProxyResolver(t)
	rootCAs, err := loadRootCAs(os.Getenv("SSL_CERT_FILE"))
	require.NoError(t, err)
	dexConfig := dexProviderConfig(Config{
		DexIssuerURL:    os.Getenv("DEX_PROXY_TEST_ISSUER"),
		DexClientID:     "client",
		DexClientSecret: "secret",
		RedirectURL:     "https://mcp-prometheus.example.com/oauth/callback",
	}, rootCAs)
	dexConfig.DiscoveryTimeout = 2 * time.Second
	_, err = dex.NewProvider(dexConfig)
	require.NoError(t, err)
}

func TestDexProviderConfigLeavesClientToMCPOAuth(t *testing.T) {
	rootCAs := x509.NewCertPool()
	dexConfig := dexProviderConfig(Config{
		DexIssuerURL:    "https://" + dexProxyHostname,
		DexClientID:     "client",
		DexClientSecret: "secret",
		RedirectURL:     "https://mcp-prometheus.example.com/oauth/callback",
	}, rootCAs)
	require.Nil(t, dexConfig.HTTPClient)
	require.Same(t, rootCAs, dexConfig.RootCAs)
}
