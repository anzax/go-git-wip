// Package cli provides the optional checkpoint command-line interface.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewCommand constructs an independent command tree for each invocation.
func NewCommand(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "go-git-wip",
		Short:         "Local Git WIP checkpoints (bootstrap; operations not implemented)",
		Version:       version,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetVersionTemplate("{{.Version}}\n")
	root.Flags().BoolP("version", "v", false, "show version")
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Show version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version)
			return err
		},
	})
	return root
}
