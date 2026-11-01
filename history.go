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
// An observed ref change during the read returns KindConflict without entries.
func (r *Repository) Reflog(ctx context.Context, ref string, limit int) ([]ReflogEntry, error) {
	if limit < 0 {
		return nil, &Error{Kind: KindInvalidOption, Op: "reflog", Message: "limit must not be negative"}
	}
	if _, err := checkpointRefBranch(ref); err != nil {
		return nil, err
	}
	if _, err := r.git(ctx, "symbolic-ref", "--quiet", ref); err == nil {
		return nil, &Error{Kind: KindInvalidRef, Op: "ref", Message: "symbolic checkpoint refs are unsupported"}
	} else if !gitExitCode(err, 1) {
		if ctx.Err() != nil || outputLimitError(err) {
			return nil, err
		}
		return nil, &Error{Kind: KindInvalidRef, Op: "ref", Message: "checkpoint ref cannot be resolved", Err: err}
	}
	refs, err := r.readRefSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	if refs[ref] == "" {
		return []ReflogEntry{}, nil
	}
	if _, err := r.validateObservedTips(ctx, refs, []string{ref}); err != nil {
		return nil, err
	}
	args := []string{"reflog", "show", "--format=%H%x00%gs%x00"}
	if limit != 0 {
		args = append(args, "--max-count="+strconv.Itoa(limit))
	}
	args = append(args, ref, "--")
	out, readErr := r.git(ctx, args...)
	if readErr != nil && (ctx.Err() != nil || outputLimitError(readErr)) {
		return nil, readErr
	}
	// Git reads the mutable ref independently of our validation. Fail closed if
	// its publication changed while the log was read, rather than return a log
	// rooted in an unvalidated replacement target. A deletion can also make the
	// log command fail, so ordinary failures need the same ref observation.
	current, err := r.git(ctx, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(symref)", ref)
	if err != nil {
		if readErr != nil && ctx.Err() == nil && !outputLimitError(err) {
			return nil, readErr
		}
		return nil, err
	}
	matched := false
	for _, line := range strings.Split(string(current), "\n") {
		fields := strings.Split(line, "\x00")
		if len(fields) == 3 && fields[0] == ref {
			matched = fields[1] == refs[ref] && fields[2] == ""
		}
	}
	if !matched {
		return nil, &Error{Kind: KindConflict, Op: "reflog", Message: "checkpoint ref changed while reading its reflog"}
	}
	if readErr != nil {
		return nil, readErr
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
