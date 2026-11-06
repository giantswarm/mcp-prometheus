package cmd

import _ "embed"

// prometheusLogo is the Prometheus logo the server advertises as its MCP icon.
// Source: github.com/cncf/artwork projects/prometheus/icon/color/prometheus-icon-color.svg
// at c33a8386bce4eabc36e1d4972e0996db4630037b, minified losslessly.
//
//go:embed prometheus.svg
var prometheusLogo []byte
