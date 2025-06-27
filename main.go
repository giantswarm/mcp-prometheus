package main

import (
	"github.com/giantswarm/mcp-prometheus/cmd"
)

// version will be set by goreleaser during build
var version = "dev"

func main() {
	// Set the version from build-time variable
	cmd.SetVersion(version)

	// Execute the root command
	cmd.Execute()
}
