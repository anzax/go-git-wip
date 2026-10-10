package cli

import (
	"encoding/json"

	"github.com/anzax/go-git-wip"
	"github.com/spf13/cobra"
)

func writeJSON(cmd *cobra.Command, value any) error {
	return json.NewEncoder(cmd.OutOrStdout()).Encode(value)
}

func (o *options) writeCapture(cmd *cobra.Command, result wip.CaptureResult) error {
	if o.json {
		return writeJSON(cmd, result)
	}
	if !result.Created {
		return printLine(cmd, "No changes")
	}
	return printLine(cmd, "Saved %s (%s)", result.Commit, result.Ref)
}

func (o *options) writeStatus(cmd *cobra.Command, status wip.Status) error {
	if o.json {
		return writeJSON(cmd, status)
	}
	branch, head := status.Repository.Branch, status.Repository.HEAD
	if branch == "" {
		branch = "(detached)"
	}
	if head == "" {
		head = "(unborn)"
	}
	history := "missing"
	if status.Tip != "" {
		history = "stale"
		if status.Active {
			history = "active"
		}
	}
	if err := printLine(cmd, "Branch %s\nHEAD %s\nHistory %s", branch, head, history); err != nil {
		return err
	}
	if status.Tip != "" {
		return printLine(cmd, "Ref %s\nTip %s\nBase %s", status.Ref, status.Tip, status.Base)
	}
	return nil
}

func (o *options) writeHistory(cmd *cobra.Command, checkpoints []wip.Checkpoint) error {
	if checkpoints == nil {
		checkpoints = []wip.Checkpoint{}
	}
	if o.json {
		return writeJSON(cmd, checkpoints)
	}
	if len(checkpoints) == 0 {
		return printLine(cmd, "No checkpoints")
	}
	for _, checkpoint := range checkpoints {
		if err := printLine(cmd, "%s %s %s", checkpoint.Commit, checkpoint.Time.Format("2006-01-02T15:04:05Z07:00"), firstLine(checkpoint.Message)); err != nil {
			return err
		}
	}
	return nil
}

func (o *options) writeRefs(cmd *cobra.Command, refs []wip.RefInfo) error {
	if refs == nil {
		refs = []wip.RefInfo{}
	}
	if o.json {
		return writeJSON(cmd, refs)
	}
	if len(refs) == 0 {
		return printLine(cmd, "No checkpoint refs")
	}
	for _, ref := range refs {
		label := ""
		if ref.Orphaned {
			label = " (orphaned)"
		}
		if err := printLine(cmd, "%s %s%s", ref.Ref, ref.Tip, label); err != nil {
			return err
		}
	}
	return nil
}

func (o *options) writeReflog(cmd *cobra.Command, entries []wip.ReflogEntry) error {
	if entries == nil {
		entries = []wip.ReflogEntry{}
	}
	if o.json {
		return writeJSON(cmd, entries)
	}
	if len(entries) == 0 {
		return printLine(cmd, "No retained reflog entries")
	}
	for _, entry := range entries {
		if err := printLine(cmd, "%s %s %s", entry.Selector, entry.Commit, entry.Message); err != nil {
			return err
		}
	}
	return nil
}

func (o *options) writeComparison(cmd *cobra.Command, comparison wip.Comparison) error {
	if comparison.Files == nil {
		comparison.Files = []wip.FileChange{}
	}
	if o.json {
		return writeJSON(cmd, comparison)
	}
	if err := printLine(cmd, "%s..%s", comparison.From, comparison.To); err != nil {
		return err
	}
	if len(comparison.Files) == 0 {
		return printLine(cmd, "No changes")
	}
	for _, file := range comparison.Files {
		var err error
		if file.Binary {
			err = printLine(cmd, "%s %q binary", file.Status, file.Path)
		} else {
			err = printLine(cmd, "%s %q +%d -%d", file.Status, file.Path, file.Added, file.Deleted)
		}
		if err != nil {
			return err
		}
	}
	if comparison.Patch != "" {
		_, err := cmd.OutOrStdout().Write([]byte(comparison.Patch))
		return err
	}
	return nil
}

func (o *options) writeDelete(cmd *cobra.Command, result wip.DeleteResult) error {
	if o.json {
		return writeJSON(cmd, result)
	}
	if result.Deleted {
		return printLine(cmd, "Deleted %s", result.Ref)
	}
	return printLine(cmd, "Already missing %s", result.Ref)
}

func (o *options) writeCleanup(cmd *cobra.Command, result wip.CleanupResult) error {
	if result.Deleted == nil {
		result.Deleted = []string{}
	}
	if result.Failures == nil {
		result.Failures = []wip.CleanupFailure{}
	}
	if o.json {
		return writeJSON(cmd, result)
	}
	for _, ref := range result.Deleted {
		if err := printLine(cmd, "Deleted %s", ref); err != nil {
			return err
		}
	}
	for _, failure := range result.Failures {
		if err := printLine(cmd, "Failed %s: %s", failure.Ref, failure.Message); err != nil {
			return err
		}
	}
	return nil
}
