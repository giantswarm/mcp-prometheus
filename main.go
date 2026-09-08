package main

import (
	"github.com/giantswarm/mcp-prometheus/cmd"
	"github.com/giantswarm/mcp-prometheus/pkg/project"
)

func main() {
	// The version comes from pkg/project, which the generated Makefile and the
	// architect-orb go-build job stamp at link time via -X ldflags.
	cmd.SetVersion(project.Version())

	// Execute the root command
	cmd.Execute()
}
