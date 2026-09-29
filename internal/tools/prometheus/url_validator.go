package prometheus

import (
	"fmt"
	"net"
	"net/url"
)

// validatePrometheusURL checks that a caller-supplied prometheus_url is safe
// to use as an HTTP target; see validateURL.
func validatePrometheusURL(raw string) error {
	return validateURL("prometheus_url", raw)
}

// validateURL checks that the caller-supplied URL parameter param is safe to
// use as an HTTP target.
//
// Permitted:
//   - Scheme must be http or https.
//   - Any hostname (DNS names, RFC-1918 addresses) — Prometheus is commonly
//     deployed on private cluster IPs and the tool is already auth-gated.
//
// Blocked:
//   - Non-HTTP(S) schemes (file://, ftp://, gopher://, …) — clear SSRF vector.
//   - The IPv4 link-local range 169.254.0.0/16 and its IPv6 equivalent
//     fe80::/10 — this is where cloud instance metadata services
//     (AWS IMDSv1, GCP, Azure) live; there is never a legitimate Prometheus
//     endpoint there.
func validateURL(param, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", param, err)
	}

	switch u.Scheme {
	case "http", "https":
		// allowed
	case "":
		return fmt.Errorf("invalid %s %q: scheme is required (use http:// or https://)", param, raw)
	default:
		return fmt.Errorf("invalid %s %q: scheme %q is not allowed (use http:// or https://)", param, raw, u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("invalid %s %q: host is required", param, raw)
	}

	ip := net.ParseIP(host)
	if ip != nil {
		// Block link-local ranges — cloud metadata services (169.254.169.254, fe80::…).
		if isLinkLocal(ip) {
			return fmt.Errorf("invalid %s %q: link-local addresses are not allowed", param, raw)
		}
	}

	return nil
}

// isLinkLocal reports whether ip falls in the IPv4 link-local range
// (169.254.0.0/16) or the IPv6 link-local range (fe80::/10).
func isLinkLocal(ip net.IP) bool {
	linkLocalV4 := &net.IPNet{
		IP:   net.IPv4(169, 254, 0, 0),
		Mask: net.CIDRMask(16, 32),
	}
	if ip4 := ip.To4(); ip4 != nil {
		return linkLocalV4.Contains(ip4)
	}
	// IPv6 link-local: fe80::/10
	if len(ip) == 16 && ip[0] == 0xfe && (ip[1]&0xc0) == 0x80 {
		return true
	}
	return false
}
