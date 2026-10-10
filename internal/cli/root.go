// Package cli provides the optional checkpoint command-line interface.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

type options struct {
	directory string
	json      bool
	started   bool
}

// NewCommand constructs an independent command tree for each invocation.
func NewCommand(version string) *cobra.Command {
	root, _ := newCommand(version)
	return root
}

func newCommand(version string) (*cobra.Command, *options) {
	opts := &options{directory: "."}
	root := &cobra.Command{
		Use:           "go-git-wip",
		Short:         "Save and inspect local Git working-tree checkpoints",
		Version:       version,
		Args:          argumentCount(0, 0),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			opts.started = true
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetVersionTemplate("{{.Version}}\n")
	root.Flags().BoolP("version", "v", false, "show version")
	root.PersistentFlags().StringVarP(&opts.directory, "directory", "C", ".", "repository invocation directory")
	root.PersistentFlags().BoolVar(&opts.json, "json", false, "emit structured JSON")
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error { return &usageError{err.Error()} })
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Show version",
		Args:  argumentCount(0, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version)
			return err
		},
	})
	root.AddCommand(opts.saveCommand(), opts.statusCommand(), opts.historyCommand(), opts.refsCommand(), opts.reflogCommand(), opts.showCommand(), opts.diffCommand(), opts.fileCommand(), opts.deleteCommand(), opts.cleanupCommand())
	return root, opts
}

func argumentCount(minimum, maximum int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < minimum || (maximum >= 0 && len(args) > maximum) {
			return &usageError{fmt.Sprintf("invalid arguments; use %s", cmd.UseLine())}
		}
		return nil
	}
}
