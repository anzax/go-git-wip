package wip

import (
	"context"
	"strconv"
	"strings"
)

// Status reports the current branch's checkpoint tip even when its base is stale.
// Detached and unborn working trees have no active history.
func (r *Repository) Status(ctx context.Context) (Status, error) {
	info, err := r.Info(ctx)
	if err != nil {
		return Status{}, err
	}
	result := Status{Repository: info}
	if info.Branch == "" {
		return result, nil
	}
	result.Ref = checkpointRef(info.Branch)
	tip, err := r.validateTip(ctx, result.Ref, info.Branch)
	if err != nil {
		return Status{}, err
	}
	result.Tip, result.Base = tip.Commit, tip.Base
	result.Active = tip.Commit != "" && info.HEAD != "" && tip.Base == info.HEAD
	return result, nil
}

// History lists one owned stack newest first, stopping before its normal base.
// The limit counts checkpoints matching Paths; zero means unlimited.
func (r *Repository) History(ctx context.Context, options HistoryOptions) ([]Checkpoint, error) {
	if options.Limit < 0 {
		return nil, &Error{Kind: KindInvalidOption, Op: "history", Message: "limit must not be negative"}
	}
	paths, err := r.normalizePaths(options.Paths)
	if err != nil {
		return nil, err
	}
	var tip Checkpoint
	if options.Revision == "" {
		status, err := r.Status(ctx)
		if err != nil {
			return nil, err
		}
		if status.Tip == "" {
			return []Checkpoint{}, nil
		}
		tip, err = r.resolveCheckpoint(ctx, status.Tip)
		if err != nil {
			return nil, err
		}
	} else {
		tip, err = r.resolveCheckpoint(ctx, options.Revision)
		if err != nil {
			return nil, err
		}
	}
	result := []Checkpoint{}
	current := tip
	for {
		parent, err := r.validateCheckpointParent(ctx, current, "history")
		if err != nil {
			return nil, err
		}
		matches := true
		if len(paths) != 0 {
			comparison, err := r.compareCommits(ctx, current.Parent, current.Commit, paths, false)
			if err != nil {
				return nil, err
			}
			matches = len(comparison.Files) != 0
		}
		if matches {
			result = append(result, current)
		}
		if (options.Limit != 0 && len(result) == options.Limit) || current.Parent == current.Base {
			return result, nil
		}
		current = parent
	}
}

// Reflog returns retained records newest first. Duplicate commit IDs remain
// distinct through their ordinal selectors. Deleting a ref can remove its log.
func (r *Repository) Reflog(ctx context.Context, ref string, limit int) ([]ReflogEntry, error) {
	if limit < 0 {
		return nil, &Error{Kind: KindInvalidOption, Op: "reflog", Message: "limit must not be negative"}
	}
	branch, err := r.validateCheckpointRef(ctx, ref)
	if err != nil {
		return nil, err
	}
	tip, err := r.validateTip(ctx, ref, branch)
	if err != nil {
		return nil, err
	}
	if tip.Commit == "" {
		return []ReflogEntry{}, nil
	}
	args := []string{"reflog", "show", "--format=%H%x00%gs%x00"}
	if limit != 0 {
		args = append(args, "--max-count="+strconv.Itoa(limit))
	}
	args = append(args, ref, "--")
	out, err := r.git(ctx, args...)
	if err != nil {
		return nil, err
	}
	result := []ReflogEntry{}
	fields := strings.Split(string(out), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		commit := strings.TrimPrefix(fields[i], "\n")
		result = append(result, ReflogEntry{
			Selector: ref + "@{" + strconv.Itoa(len(result)) + "}",
			Commit:   commit, Message: fields[i+1],
		})
	}
	return result, nil
}
