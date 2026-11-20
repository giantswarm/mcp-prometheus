package oauth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServerConfigEnablesClientIDMetadataDocuments(t *testing.T) {
	require.True(t, serverConfig(Config{}, nil).EnableClientIDMetadataDocuments)
}
