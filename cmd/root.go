package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "mcp-prometheus",
	Short: "MCP server for Prometheus metrics and queries",
	Long: `mcp-prometheus is a Model Context Protocol (MCP) server that provides
access to Prometheus metrics and queries through standardized MCP interfaces.

This allows AI assistants to execute PromQL queries, discover metrics,
and analyze metrics data from your Prometheus instance.

The server supports various authentication methods including basic auth
and bearer tokens, and can be configured through environment variables.`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

// SetVersion sets the version for the root command
func SetVersion(version string) {
	rootCmd.Version = version
}

func init() {
	// Add subcommands
	rootCmd.AddCommand(newServeCmd())
	rootCmd.AddCommand(newVersionCmd())
} 