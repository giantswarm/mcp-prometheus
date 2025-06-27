package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newVersionCmd creates a new version command
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version number",
		Long:  `Print the version number of mcp-prometheus`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("mcp-prometheus %s\n", rootCmd.Version)
		},
	}
}
