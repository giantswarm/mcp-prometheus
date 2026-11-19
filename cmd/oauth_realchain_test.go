package cmd

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/mcp-oauth/handler"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"

	"github.com/giantswarm/mcp-prometheus/internal/oauth"
)

// privateIdP is an OIDC issuer the production chain reaches as it reaches
// Dex on a private management cluster: a hostname that resolves to an
// RFC 1918 address of this machine, over TLS with its own CA. The JWKS is on
// the issuer's host, or on a second host when keysHost is set.
type privateIdP struct {
	issuer string
	key    *rsa.PrivateKey
	caFile string
}

const (
	privateIssuerHost = "idp.test"
	privateKeysHost   = "keys.test"
	privateTestKID    = "private-key"
	privateClientID   = "muster-client"
)

// newPrivateIdP serves discovery and JWKS on an RFC 1918 address of this
// machine and answers DNS for idp.test and keys.test with it, through
// net.DefaultResolver, which mcp-oauth's guarded and plain dialers both use.
// It skips when the machine has no RFC 1918 IPv4 address.
func newPrivateIdP(t *testing.T, keysHost string) *privateIdP {
	t.Helper()
	ip := privateIPv4(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	idp := &privateIdP{key: key}

	listener, err := net.Listen("tcp", net.JoinHostPort(ip.String(), "0"))
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	origin := func(host string) string { return "https://" + net.JoinHostPort(host, strconv.Itoa(port)) }
	idp.issuer = origin(privateIssuerHost) + "/dex"
	jwksURI := idp.issuer + "/keys"
	if keysHost != "" {
		jwksURI = origin(keysHost) + "/dex/keys"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/dex/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                idp.issuer,
			"authorization_endpoint":                idp.issuer + "/auth",
			"token_endpoint":                        idp.issuer + "/token",
			"userinfo_endpoint":                     idp.issuer + "/userinfo",
			"jwks_uri":                              jwksURI,
			"response_types_supported":              []string{"code"},
			"scopes_supported":                      []string{"openid", "email", "profile", "groups", "offline_access"},
			"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
			"code_challenge_methods_supported":      []string{"S256"},
			"token_endpoint_auth_methods_supported": []string{"client_secret_basic"},
		})
	})
	mux.HandleFunc("/dex/keys", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: privateTestKID, Algorithm: string(jose.RS256), Use: "sig"},
		}})
	})

	cert := selfSignedFor(t, []string{privateIssuerHost, privateKeysHost})
	srv := httptest.NewUnstartedServer(mux)
	srv.Listener = listener
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	idp.caFile = filepath.Join(t.TempDir(), "ca.crt")
	require.NoError(t, os.WriteFile(idp.caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0o600))

	bypassProxy(t, privateIssuerHost, privateKeysHost)
	resolveTestHosts(t, ip, privateIssuerHost, privateKeysHost)
	return idp
}

// privateIPv4 is an RFC 1918 IPv4 address of this machine.
func privateIPv4(t *testing.T) net.IP {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	require.NoError(t, err)
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && n.IP.IsPrivate() {
			return n.IP.To4()
		}
	}
	t.Skip("no RFC 1918 IPv4 address on this machine")
	return nil
}

func selfSignedFor(t *testing.T, hosts []string) tls.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: hosts[0]},
		DNSNames:              hosts,
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// bypassProxy clears the inherited proxy variables, then skips when the
// process-wide proxy setting, read once by net/http, still routes the test
// hosts through a proxy that cannot resolve them.
func bypassProxy(t *testing.T, hosts ...string) {
	t.Helper()
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		t.Setenv(name, "")
	}
	for _, h := range hosts {
		req, err := http.NewRequest(http.MethodGet, "https://"+h+"/", nil)
		require.NoError(t, err)
		if proxy, _ := http.ProxyFromEnvironment(req); proxy != nil {
			t.Skipf("the process proxy setting routes %s through %s", h, proxy.Host)
		}
	}
}

// resolveTestHosts answers A queries for hosts with ip, through a DNS server
// on loopback that net.DefaultResolver uses for the rest of the test; every
// other name gets NXDOMAIN.
func resolveTestHosts(t *testing.T, ip net.IP, hosts ...string) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	known := map[string]bool{}
	for _, h := range hosts {
		known[h+"."] = true
	}
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			var parser dnsmessage.Parser
			header, err := parser.Start(buf[:n])
			if err != nil {
				continue
			}
			question, err := parser.Question()
			if err != nil {
				continue
			}
			reply := dnsmessage.Message{
				Header:    dnsmessage.Header{ID: header.ID, Response: true, Authoritative: true, RCode: dnsmessage.RCodeSuccess},
				Questions: []dnsmessage.Question{question},
			}
			name := strings.ToLower(question.Name.String())
			switch {
			case !known[name]:
				reply.RCode = dnsmessage.RCodeNameError
			case question.Type == dnsmessage.TypeA:
				var a [4]byte
				copy(a[:], ip.To4())
				reply.Answers = []dnsmessage.Resource{{
					Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60},
					Body:   &dnsmessage.AResource{A: a},
				}}
			}
			packed, err := reply.Pack()
			if err == nil {
				_, _ = conn.WriteTo(packed, from)
			}
		}
	}()

	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "udp", conn.LocalAddr().String())
	}}
	t.Cleanup(func() { net.DefaultResolver = previous })
}

func (p *privateIdP) idToken(t *testing.T, aud []string) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: p.key},
		(&jose.SignerOptions{}).WithHeader("kid", privateTestKID).WithType("JWT"))
	require.NoError(t, err)
	claims := jwt.Claims{Issuer: p.issuer, Subject: "sub-admin", Audience: jwt.Audience(aud),
		Expiry: jwt.NewNumericDate(time.Now().Add(30 * time.Minute)), IssuedAt: jwt.NewNumericDate(time.Now().Add(-time.Minute))}
	extra := map[string]any{"email": "admin@lab.local", "email_verified": true, "name": "Lab Admin", "groups": []string{"platform-admins"}}
	tok, err := jwt.Signed(signer).Claims(claims).Claims(extra).Serialize()
	require.NoError(t, err)
	return tok
}

// setEnv sets the environment the chart renders for a Dex installation that
// trusts tokens forwarded for privateClientID.
func (p *privateIdP) setEnv(t *testing.T, jwksHosts string) {
	t.Helper()
	for name, value := range map[string]string{
		"MCP_OAUTH_ISSUER":                      "https://mcp.example.com",
		"MCP_OAUTH_ENCRYPTION_KEY":              "",
		"MCP_OAUTH_PROVIDER":                    "dex",
		"MCP_OAUTH_ALLOW_PUBLIC_REGISTRATION":   "false",
		"MCP_OAUTH_ALLOW_PRIVATE_IP_JWKS":       "false",
		"MCP_OAUTH_ALLOW_PRIVATE_URLS":          "",
		"OAUTH_ALLOW_PRIVATE_URLS":              "",
		"MCP_OAUTH_ALLOW_PRIVATE_IP_JWKS_HOSTS": jwksHosts,
		"OAUTH_REDIRECT_URL":                    "https://mcp.example.com/oauth/callback",
		"OAUTH_TRUSTED_AUDIENCES":               privateClientID,
		"OAUTH_STORAGE":                         "",
		"DEX_ISSUER_URL":                        p.issuer,
		"DEX_CLIENT_ID":                         "mcp-prometheus",
		"DEX_CLIENT_SECRET":                     "lab-only",
		"DEX_CA_FILE":                           p.caFile,
	} {
		t.Setenv(name, value)
	}
}

// callThroughChain sends one request to /mcp with bearer through the
// production OAuth configuration, handler and routes, and returns the status
// and the caller the MCP handler saw.
func callThroughChain(t *testing.T, bearer string) (int, string) {
	t.Helper()
	cfg, err := oauth.ConfigFromEnv()
	require.NoError(t, err)
	h, cleanup, err := oauth.NewHandler(t.Context(), cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	t.Cleanup(cleanup)

	var caller string
	mux := http.NewServeMux()
	registerOAuthRoutes(mux, h, "/mcp", map[string]http.Handler{
		"/mcp": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if info, ok := handler.UserInfoFromContext(r.Context()); ok {
				caller = info.Email
			}
			w.WriteHeader(http.StatusNoContent)
		}),
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code, caller
}

// TestForwardedTokenThroughTheProductionChain validates a forwarded id_token
// against a Dex on a private address with no private-address setting: the
// issuer host's JWKS is allowed by the library.
func TestForwardedTokenThroughTheProductionChain(t *testing.T) {
	idp := newPrivateIdP(t, "")
	idp.setEnv(t, "")

	code, caller := callThroughChain(t, idp.idToken(t, []string{privateClientID}))
	require.Equal(t, http.StatusNoContent, code)
	require.Equal(t, "admin@lab.local", caller)

	code, _ = callThroughChain(t, idp.idToken(t, []string{"someone-else"}))
	require.Equal(t, http.StatusUnauthorized, code, "an untrusted audience is refused")

	code, _ = callThroughChain(t, "")
	require.Equal(t, http.StatusUnauthorized, code, "no token, no MCP")
}

// TestJWKSOnAnotherPrivateHost pins the split-host case: a jwks_uri on a host
// other than the issuer's that resolves privately is refused until that host
// is listed in MCP_OAUTH_ALLOW_PRIVATE_IP_JWKS_HOSTS.
func TestJWKSOnAnotherPrivateHost(t *testing.T) {
	idp := newPrivateIdP(t, privateKeysHost)
	token := idp.idToken(t, []string{privateClientID})

	idp.setEnv(t, "")
	code, _ := callThroughChain(t, token)
	require.Equal(t, http.StatusUnauthorized, code, "an unlisted private JWKS host is refused")

	idp.setEnv(t, privateKeysHost)
	code, caller := callThroughChain(t, token)
	require.Equal(t, http.StatusNoContent, code)
	require.Equal(t, "admin@lab.local", caller)
}
