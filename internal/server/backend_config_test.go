package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackendURLOverrideEnvironment(t *testing.T) {
	for _, test := range []struct {
		value             string
		disabled, invalid bool
	}{
		{value: "true"}, {value: "false", disabled: true}, {value: "invalid", invalid: true},
	} {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv("MCP_ALLOW_BACKEND_URL_OVERRIDES", test.value)
			serverContext, err := NewServerContext(t.Context())
			if test.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			defer func() { require.NoError(t, serverContext.Shutdown()) }()
			require.Equal(t, test.disabled, serverContext.PrometheusConfig().DisableURLOverrides)
		})
	}
}
