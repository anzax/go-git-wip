package cli

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/anzax/go-git-wip"
	"github.com/spf13/cobra"
)

func (o *options) repository(cmd *cobra.Command) (*wip.Repository, error) {
	return wip.Open(cmd.Context(), o.directory)
}

func (o *options) saveCommand() *cobra.Command {
	var capture wip.CaptureOptions
	cmd := &cobra.Command{
		Use:   "save [PATH...]",
		Short: "Save tracked and non-ignored untracked files as a checkpoint",
		Long: "Save tracked and non-ignored untracked files as a checkpoint.\n" +
			"No paths selects the whole repository; every literal path must match eligible content.\n" +
			"Use -m TEXT for a message.",
		Args: argumentCount(0, -1),
	}
	cmd.Flags().BoolVar(&capture.Baseline, "baseline", false, "force a checkpoint even when unchanged")
	cmd.Flags().BoolVar(&capture.TrackedOnly, "tracked-only", false, "exclude untracked files (staged additions remain included)")
	cmd.Flags().BoolVar(&capture.IncludeIgnored, "ignored", false, "also include ignored untracked files")
	cmd.Flags().StringVarP(&capture.Message, "message", "m", "", "checkpoint message")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if capture.TrackedOnly && capture.IncludeIgnored {
			return &usageError{"--tracked-only and --ignored cannot be combined"}
		}
		r, err := o.repository(cmd)
		if err != nil {
			return err
		}
		capture.Paths = args
		result, err := r.Capture(cmd.Context(), capture)
		if err != nil {
			return err
		}
		return o.writeCapture(cmd, result)
	}
	return cmd
}

func (o *options) statusCommand() *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Show current checkpoint history state", Args: argumentCount(0, 0), RunE: func(cmd *cobra.Command, args []string) error {
		r, err := o.repository(cmd)
		if err != nil {
			return err
		}
		result, err := r.Status(cmd.Context())
		if err != nil {
			return err
		}
		return o.writeStatus(cmd, result)
	}}
}

func (o *options) historyCommand() *cobra.Command {
	var history wip.HistoryOptions
	cmd := &cobra.Command{Use: "history [REVISION]", Short: "List an owned checkpoint stack", Args: argumentCount(0, 1)}
	cmd.Flags().IntVar(&history.Limit, "limit", 0, "maximum matching checkpoints; zero is unlimited")
	cmd.Flags().StringArrayVar(&history.Paths, "path", nil, "literal path filter (repeatable)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if history.Limit < 0 {
			return &usageError{"--limit must not be negative"}
		}
		if len(args) == 1 {
			history.Revision = args[0]
		}
		r, err := o.repository(cmd)
		if err != nil {
			return err
		}
		result, err := r.History(cmd.Context(), history)
		if err != nil {
			return err
		}
		return o.writeHistory(cmd, result)
	}
	return cmd
}

func (o *options) refsCommand() *cobra.Command {
	return &cobra.Command{Use: "refs", Short: "List checkpoint refs, including orphans", Args: argumentCount(0, 0), RunE: func(cmd *cobra.Command, args []string) error {
		r, err := o.repository(cmd)
		if err != nil {
			return err
		}
		result, err := r.Refs(cmd.Context())
		if err != nil {
			return err
		}
		return o.writeRefs(cmd, result)
	}}
}

func (o *options) reflogCommand() *cobra.Command {
	var limit int
	cmd := &cobra.Command{Use: "reflog [REF]", Short: "List retained checkpoint reflog records", Args: argumentCount(0, 1)}
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum records; zero is unlimited")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if limit < 0 {
			return &usageError{"--limit must not be negative"}
		}
		r, err := o.repository(cmd)
		if err != nil {
			return err
		}
		var ref string
		if len(args) == 1 {
			ref = args[0]
		} else {
			info, err := r.Info(cmd.Context())
			if err != nil {
				return err
			}
			if info.Branch == "" {
				return &wip.Error{Kind: wip.KindUnsupported, Op: "reflog", Message: "detached HEAD requires an explicit checkpoint ref"}
			}
			ref = "refs/wip/" + info.Branch
		}
		result, err := r.Reflog(cmd.Context(), ref, limit)
		if err != nil {
			return err
		}
		return o.writeReflog(cmd, result)
	}
	return cmd
}

func (o *options) showCommand() *cobra.Command {
	var compare wip.CompareOptions
	cmd := &cobra.Command{Use: "show REVISION", Short: "Inspect a checkpoint relative to its parent", Args: argumentCount(1, 1)}
	comparisonFlags(cmd, &compare)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		r, err := o.repository(cmd)
		if err != nil {
			return err
		}
		result, err := r.Inspect(cmd.Context(), args[0], compare)
		if err != nil {
			return err
		}
		return o.writeComparison(cmd, result)
	}
	return cmd
}

func (o *options) diffCommand() *cobra.Command {
	var compare wip.CompareOptions
	cmd := &cobra.Command{Use: "diff FROM TO", Short: "Compare two commit revisions", Args: argumentCount(2, 2)}
	comparisonFlags(cmd, &compare)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		r, err := o.repository(cmd)
		if err != nil {
			return err
		}
		result, err := r.Compare(cmd.Context(), args[0], args[1], compare)
		if err != nil {
			return err
		}
		return o.writeComparison(cmd, result)
	}
	return cmd
}

func comparisonFlags(cmd *cobra.Command, compare *wip.CompareOptions) {
	cmd.Flags().StringArrayVar(&compare.Paths, "path", nil, "literal path filter (repeatable)")
	cmd.Flags().BoolVar(&compare.IncludePatch, "patch", false, "include the textual Git patch")
}

func (o *options) fileCommand() *cobra.Command {
	return &cobra.Command{Use: "file REVISION PATH", Short: "Read exact file bytes at a commit", Args: argumentCount(2, 2), RunE: func(cmd *cobra.Command, args []string) error {
		r, err := o.repository(cmd)
		if err != nil {
			return err
		}
		content, err := r.ReadFile(cmd.Context(), args[0], args[1])
		if err != nil {
			return err
		}
		if o.json {
			return writeJSON(cmd, struct {
				Revision string `json:"revision"`
				Path     string `json:"path"`
				Content  string `json:"content_base64"`
			}{Revision: args[0], Path: args[1], Content: base64.StdEncoding.EncodeToString(content)})
		}
		_, err = cmd.OutOrStdout().Write(content)
		return err
	}}
}

func (o *options) deleteCommand() *cobra.Command {
	var expected string
	var yes bool
	cmd := &cobra.Command{Use: "delete REF --expect TIP --yes", Short: "Conditionally delete one owned checkpoint ref", Args: argumentCount(1, 1)}
	cmd.Flags().StringVar(&expected, "expect", "", "full observed checkpoint tip ID (required)")
	cmd.Flags().BoolVar(&yes, "yes", false, "approve deletion without a prompt (required)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if !yes || expected == "" {
			return &usageError{"delete requires --yes and --expect TIP"}
		}
		r, err := o.repository(cmd)
		if err != nil {
			return err
		}
		result, err := r.Delete(cmd.Context(), wip.DeleteOptions{Ref: args[0], ExpectedTip: expected})
		if err != nil {
			return err
		}
		return o.writeDelete(cmd, result)
	}
	return cmd
}

func (o *options) cleanupCommand() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{Use: "cleanup [REF=TIP...]", Short: "Preview or explicitly delete orphaned checkpoint refs", Args: argumentCount(0, -1)}
	cmd.Flags().BoolVar(&yes, "yes", false, "approve deletion of supplied targets without a prompt")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if (yes && len(args) == 0) || (!yes && len(args) != 0) {
			return &usageError{"cleanup deletion requires explicit REF=TIP targets and --yes"}
		}
		targets := make([]wip.DeleteOptions, 0, len(args))
		for _, arg := range args {
			separator := strings.LastIndexByte(arg, '=')
			if separator <= 0 || separator == len(arg)-1 {
				return &usageError{"cleanup targets must have the form REF=TIP"}
			}
			targets = append(targets, wip.DeleteOptions{Ref: arg[:separator], ExpectedTip: arg[separator+1:]})
		}
		r, err := o.repository(cmd)
		if err != nil {
			return err
		}
		if len(targets) == 0 {
			refs, err := r.Refs(cmd.Context())
			if err != nil {
				return err
			}
			orphaned := []wip.RefInfo{}
			for _, ref := range refs {
				if ref.Orphaned {
					orphaned = append(orphaned, ref)
				}
			}
			return o.writeRefs(cmd, orphaned)
		}
		result, operationErr := r.Cleanup(cmd.Context(), targets)
		if operationErr == nil || len(result.Deleted) != 0 || len(result.Failures) != 0 {
			if err := o.writeCleanup(cmd, result); err != nil {
				return err
			}
		}
		if operationErr != nil {
			return operationErr
		}
		if len(result.Failures) != 0 {
			return &partialCleanupError{count: len(result.Failures)}
		}
		return nil
	}
	return cmd
}

func firstLine(message string) string {
	line, _, _ := strings.Cut(message, "\n")
	return line
}

func printLine(cmd *cobra.Command, format string, values ...any) error {
	_, err := fmt.Fprintf(cmd.OutOrStdout(), format+"\n", values...)
	return err
}
