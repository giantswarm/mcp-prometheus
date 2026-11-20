package prometheus

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/mcp-prometheus/internal/server"
)

const (
	testRedirectBackend   = "redirect"
	testConfiguredBackend = "configured"
	testOverridesDisabled = "disabled"
	testBackendSecret     = "secret"
)

func TestBackendCredentialIsolation(t *testing.T) {
	for _, authentication := range []string{"bearer", "basic"} {
		for _, tool := range []string{"execute_query", toolGetAlertmanagerAlerts, "check_ready"} {
			for _, destination := range []string{testConfiguredBackend, "other host", "other path", testOverridesDisabled, testRedirectBackend} {
				t.Run(authentication+"/"+tool+"/"+destination, func(t *testing.T) {
					var received atomic.Int32
					var authorization atomic.Value
					var tenant atomic.Value
					receiver := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
						received.Add(1)
						authorization.Store(request.Header.Get("Authorization"))
						tenant.Store(request.Header.Get("X-Scope-OrgID"))
						switch tool {
						case toolGetAlertmanagerAlerts:
							_, _ = writer.Write([]byte(`[]`))
						case "check_ready":
							_, _ = writer.Write([]byte(`ready`))
						default:
							_, _ = writer.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
						}
					}))
					defer receiver.Close()
					redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
						http.Redirect(writer, request, receiver.URL+"/stolen", http.StatusFound)
					}))
					defer redirect.Close()
					configured := receiver.URL + "/prometheus"
					requested := configured
					switch destination {
					case "other host", testOverridesDisabled:
						configured = redirect.URL + "/prometheus"
					case "other path":
						requested = receiver.URL + "/another"
					case testRedirectBackend:
						configured, requested = redirect.URL+"/prometheus", redirect.URL+"/prometheus"
					}
					configuration := server.PrometheusConfig{URL: configured, AlertmanagerURL: configured, OrgID: "default-tenant", DisableURLOverrides: destination == testOverridesDisabled}
					expectedAuthorization := "Bearer configured-secret"
					if authentication == "bearer" {
						configuration.Token = "configured-secret"
					} else {
						configuration.Username, configuration.Password = "backend-user", "backend-password"
						request := httptest.NewRequest(http.MethodGet, "/", nil)
						request.SetBasicAuth(configuration.Username, configuration.Password)
						expectedAuthorization = request.Header.Get("Authorization")
					}
					serverContext, err := server.NewServerContext(t.Context(), server.WithPrometheusConfig(configuration))
					require.NoError(t, err)
					defer func() { require.NoError(t, serverContext.Shutdown()) }()
					toolServer := mcpserver.NewMCPServer("test", "1.0.0", mcpserver.WithToolCapabilities(true))
					require.NoError(t, RegisterPrometheusTools(toolServer, serverContext))
					arguments := map[string]any{"query": "up", "prometheus_url": requested}
					if tool == toolGetAlertmanagerAlerts {
						arguments = map[string]any{"alertmanager_url": requested}
					}
					encodedArguments, err := json.Marshal(arguments)
					require.NoError(t, err)
					message := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, tool, encodedArguments))
					endpoint := httptest.NewServer(mcpserver.NewStreamableHTTPServer(toolServer, mcpserver.WithStateLess(true)))
					defer endpoint.Close()
					request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, endpoint.URL+"/mcp", bytes.NewReader(message))
					require.NoError(t, err)
					request.Header.Set("Content-Type", "application/json")
					request.Header.Set("Accept", "application/json, text/event-stream")
					request.Header.Set("MCP-Protocol-Version", "2025-11-25")
					response, err := endpoint.Client().Do(request)
					require.NoError(t, err)
					defer func() { require.NoError(t, response.Body.Close()) }()
					require.Equal(t, http.StatusOK, response.StatusCode)
					encoded, err := io.ReadAll(response.Body)
					require.NoError(t, err)
					if destination == testOverridesDisabled || destination == testRedirectBackend {
						require.Zero(t, received.Load())
						require.Contains(t, string(encoded), `"isError":true`)
						return
					}
					require.Positive(t, received.Load(), string(encoded))
					require.NotContains(t, string(encoded), `"isError":true`)
					if destination == testConfiguredBackend {
						require.Equal(t, expectedAuthorization, authorization.Load())
						require.Equal(t, "default-tenant", tenant.Load())
					} else {
						require.Equal(t, "", authorization.Load())
						require.Equal(t, "", tenant.Load())
					}
				})
			}
		}
	}
}

func TestBackendSelection(t *testing.T) {
	for _, requested := range []string{"https://example.test:443/prometheus/", "https://EXAMPLE.test/prometheus"} {
		configuration, err := selectBackend(server.PrometheusConfig{Token: testBackendSecret, DisableURLOverrides: true}, "https://example.test/prometheus", requested)
		require.NoError(t, err)
		require.Equal(t, testBackendSecret, configuration.Token)
	}
	for _, requested := range []string{"http://example.test/prometheus", "https://example.test:444/prometheus", "https://example.test/other", "https://user@example.test/prometheus", "https://example.test/prometheus#fragment", "https://example.test/prometheus/../other", "https://example.test/prometheus%2fother"} {
		_, err := selectBackend(server.PrometheusConfig{DisableURLOverrides: true}, "https://example.test/prometheus", requested)
		require.Error(t, err)
	}
	configuration, err := selectBackend(server.PrometheusConfig{Token: testBackendSecret, OrgID: "tenant"}, "", "http://example.test")
	require.NoError(t, err)
	require.Empty(t, configuration.Token)
	require.Empty(t, configuration.OrgID)
}

func TestBackendTransportRejectsEscapes(t *testing.T) {
	var received atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { received.Add(1) }))
	defer receiver.Close()
	backend, err := NewClient(server.PrometheusConfig{URL: receiver.URL + "/prometheus", Token: testBackendSecret}, discardLogger())
	require.NoError(t, err)
	for _, target := range []string{receiver.URL + "/other", receiver.URL + "/prometheus/../other"} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		_, err := backend.httpClient.Do(request.WithContext(t.Context()))
		require.Error(t, err)
	}
	require.Zero(t, received.Load())
	parsed, err := url.Parse("https://example.test")
	require.NoError(t, err)
	require.False(t, sameOrigin(parsed, &url.URL{Scheme: "http", Host: "example.test"}))
}

func TestBackendHTTPSDowngrade(t *testing.T) {
	var received atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { received.Add(1) }))
	defer receiver.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, receiver.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	backend, err := NewClient(server.PrometheusConfig{URL: redirect.URL, Token: testBackendSecret, TLSSkipVerify: true}, discardLogger())
	require.NoError(t, err)
	_, err = backend.ExecuteQuery(t.Context(), "up", "")
	require.ErrorContains(t, err, "redirects are disabled")
	_, err = backend.CheckReady(t.Context())
	require.ErrorContains(t, err, "redirects are disabled")
	require.Zero(t, received.Load())
}
