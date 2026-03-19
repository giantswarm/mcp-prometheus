package prometheus

import (
	"testing"
)

func TestValidatePrometheusURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		// Allowed
		{name: "http localhost", url: "http://localhost:9090", wantErr: false},
		{name: "https hostname", url: "https://prometheus.example.com", wantErr: false},
		{name: "http private IP", url: "http://10.0.0.1:9090", wantErr: false},
		{name: "http 192.168 IP", url: "http://192.168.1.100:9090/prometheus", wantErr: false},
		{name: "http 172.16 IP", url: "http://172.16.0.1:9090", wantErr: false},
		{name: "http with path", url: "http://mimir-gateway/prometheus", wantErr: false},

		// Blocked — bad scheme
		{name: "file scheme", url: "file:///etc/passwd", wantErr: true},
		{name: "ftp scheme", url: "ftp://prometheus.example.com", wantErr: true},
		{name: "no scheme", url: "prometheus.example.com:9090", wantErr: true},
		{name: "empty string", url: "", wantErr: true},

		// Blocked — link-local (cloud metadata services)
		{name: "AWS metadata IPv4", url: "http://169.254.169.254/latest/meta-data/", wantErr: true},
		{name: "link-local range", url: "http://169.254.1.1:9090", wantErr: true},
		{name: "IPv6 link-local", url: "http://[fe80::1]:9090", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePrometheusURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePrometheusURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}
