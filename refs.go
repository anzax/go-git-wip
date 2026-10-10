package wip

import (
	"context"
	"errors"
	"sort"
	"strings"
)

// Refs lists validated owned refs in lexicographic order, including orphans.
// An invalid owned entry fails the operation instead of silently disappearing.
func (r *Repository) Refs(ctx context.Context) ([]RefInfo, error) {
	out, err := r.git(ctx, "show-ref")
	if err != nil && !gitExitCode(err, 1) {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, &Error{Kind: KindInvalidRef, Op: "refs", Message: "repository contains an invalid ref", Err: err}
	}
	names := []string{}
	for _, line := range strings.Split(string(out), "\n") {
		_, name, found := strings.Cut(line, " ")
		if found && strings.HasPrefix(name, "refs/wip/") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	result := []RefInfo{}
	for _, ref := range names {
		branch, err := r.validateCheckpointRef(ctx, ref)
		if err != nil {
			return nil, err
		}
		tip, err := r.validateTip(ctx, ref, branch)
		if err != nil {
			return nil, err
		}
		if tip.Commit == "" {
			continue // The ref was removed after enumeration.
		}
		exists, err := r.branchExists(ctx, branch)
		if err != nil {
			return nil, err
		}
		result = append(result, RefInfo{Ref: ref, Branch: branch, Tip: tip.Commit, Base: tip.Base, Orphaned: !exists})
	}
	return result, nil
}

func (r *Repository) branchExists(ctx context.Context, branch string) (bool, error) {
	_, err := r.git(ctx, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if gitExitCode(err, 1) {
		return false, nil
	}
	return err == nil, err
}

// Delete conditionally removes an owned ref using its observed full tip ID.
// Missing refs succeed with Deleted false; an updated tip returns KindConflict.
func (r *Repository) Delete(ctx context.Context, options DeleteOptions) (DeleteResult, error) {
	result := DeleteResult{Ref: options.Ref}
	branch, err := r.validateCheckpointRef(ctx, options.Ref)
	if err != nil {
		return result, err
	}
	valid, err := r.fullObjectID(ctx, options.ExpectedTip)
	if err != nil {
		return result, err
	}
	if !valid {
		return result, &Error{Kind: KindInvalidOption, Op: "delete", Message: "expected tip must be a full object ID"}
	}
	tip, err := r.validateTip(ctx, options.Ref, branch)
	if err != nil {
		return result, err
	}
	if tip.Commit == "" {
		return result, nil
	}
	if tip.Commit != options.ExpectedTip {
		return result, &Error{Kind: KindConflict, Op: "delete", Message: "checkpoint ref changed since it was observed"}
	}
	_, err = r.git(ctx, "update-ref", "--no-deref", "-d", options.Ref, options.ExpectedTip)
	if err != nil {
		if ctx.Err() != nil {
			return result, err
		}
		current, readErr := r.validateTip(ctx, options.Ref, branch)
		if readErr == nil && current.Commit != options.ExpectedTip {
			return result, &Error{Kind: KindConflict, Op: "delete", Message: "checkpoint ref changed during deletion", Err: err}
		}
		return result, err
	}
	result.Deleted = true
	return result, nil
}

// Cleanup deletes only explicitly selected refs whose normal branches are absent.
// Per-target failures are retained; cancellation returns the partial result and error.
func (r *Repository) Cleanup(ctx context.Context, targets []DeleteOptions) (CleanupResult, error) {
	result := CleanupResult{Deleted: []string{}, Failures: []CleanupFailure{}}
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return result, &Error{Kind: KindGit, Op: "cleanup", Message: "operation cancelled", Err: err}
		}
		branch, err := r.validateCheckpointRef(ctx, target.Ref)
		if err == nil {
			var exists bool
			exists, err = r.branchExists(ctx, branch)
			if err == nil && exists {
				err = &Error{Kind: KindInvalidOption, Op: "cleanup", Message: "checkpoint ref is not orphaned"}
			}
		}
		if err == nil {
			var deleted DeleteResult
			deleted, err = r.Delete(ctx, target)
			if err == nil && deleted.Deleted {
				result.Deleted = append(result.Deleted, deleted.Ref)
			}
		}
		if err != nil {
			result.Failures = append(result.Failures, CleanupFailure{Ref: target.Ref, Message: err.Error(), Err: err})
			var failure *Error
			if ctx.Err() != nil || (errors.As(err, &failure) && failure.Kind == KindGit) {
				return result, err
			}
		}
	}
	return result, nil
}
