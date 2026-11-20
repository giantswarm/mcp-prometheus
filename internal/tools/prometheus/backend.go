package prometheus

import (
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/giantswarm/mcp-prometheus/internal/server"
)

const backendHTTPS = "https"

func backendURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid backend URL")
	}
	if (parsed.Scheme != "http" && parsed.Scheme != backendHTTPS) || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return nil, fmt.Errorf("backend URL requires HTTP(S), a host, and no userinfo, query or fragment")
	}
	if strings.Contains(parsed.Path, "\\") || strings.Contains(parsed.RawPath, "%") {
		return nil, fmt.Errorf("backend URL contains an ambiguous path")
	}
	for segment := range strings.SplitSeq(parsed.Path, "/") {
		if segment == "." || segment == ".." {
			return nil, fmt.Errorf("backend URL contains traversal")
		}
	}
	parsed.Path = path.Clean("/" + parsed.Path)
	parsed.RawPath = ""
	return parsed, nil
}

func sameOrigin(left, right *url.URL) bool {
	port := func(value *url.URL) string {
		if value.Port() != "" {
			return value.Port()
		}
		if value.Scheme == backendHTTPS {
			return "443"
		}
		return "80"
	}
	return left.Scheme == right.Scheme && strings.EqualFold(left.Hostname(), right.Hostname()) && port(left) == port(right)
}

func selectBackend(config server.PrometheusConfig, configured, requested string) (server.PrometheusConfig, error) {
	if requested == "" {
		requested = configured
	}
	destination, err := backendURL(requested)
	if err != nil {
		return config, err
	}
	trusted := false
	if configured != "" {
		original, parseErr := backendURL(configured)
		if parseErr != nil {
			return config, parseErr
		}
		trusted = sameOrigin(original, destination) && original.Path == destination.Path
	}
	if !trusted {
		if config.DisableURLOverrides {
			return config, fmt.Errorf("backend URL overrides are disabled")
		}
		config.Token, config.Username, config.Password, config.OrgID = "", "", "", ""
		config.TLSSkipVerify, config.TLSCACert = false, ""
	}
	config.URL = destination.String()
	return config, nil
}

// backendTransport confines authenticated requests to the selected service and its readiness endpoints.
type backendTransport struct {
	base      *url.URL
	transport http.RoundTripper
}

func (transport *backendTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	destination := request.URL
	requestPath := path.Clean("/" + destination.Path)
	basePath := transport.base.Path
	allowedPath := basePath == "/" || requestPath == basePath || strings.HasPrefix(requestPath, basePath+"/")
	readiness := request.Method == http.MethodGet && (destination.Path == "/-/ready" || destination.Path == "/ready")
	if !sameOrigin(transport.base, destination) || (!allowedPath && !readiness) || destination.User != nil || destination.Fragment != "" {
		return nil, fmt.Errorf("request is outside the selected backend")
	}
	return transport.transport.RoundTrip(request.Clone(request.Context()))
}

func rejectBackendRedirect(_ *http.Request, _ []*http.Request) error {
	return fmt.Errorf("backend redirects are disabled; configure the final backend URL")
}
