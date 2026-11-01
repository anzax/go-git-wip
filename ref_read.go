package wip

import (
	"context"
	"strings"
)

// readRefSnapshot retains show-ref's strict treatment of malformed refs, including
// refs outside our namespace. Each operation gets its own immutable observations.
func (r *Repository) readRefSnapshot(ctx context.Context) (map[string]string, error) {
	out, err := r.git(ctx, "show-ref")
	if err != nil && !gitExitCode(err, 1) {
		if ctx.Err() != nil || outputLimitError(err) {
			return nil, err
		}
		return nil, &Error{Kind: KindInvalidRef, Op: "ref", Message: "repository contains an invalid ref", Err: err}
	}
	refs := make(map[string]string)
	for _, line := range strings.Split(string(out), "\n") {
		oid, name, found := strings.Cut(line, " ")
		if found {
			refs[name] = oid
		}
	}
	return refs, nil
}

func checkpointRefBranch(ref string) (string, error) {
	if !strings.HasPrefix(ref, "refs/wip/") || ref == "refs/wip/" {
		return "", &Error{Kind: KindInvalidRef, Op: "ref", Message: "expected a complete refs/wip/ ref"}
	}
	branch := strings.TrimPrefix(ref, "refs/wip/")
	if !validCheckpointBranch(branch) {
		return "", &Error{Kind: KindInvalidRef, Op: "ref", Message: "invalid checkpoint ref name"}
	}
	return branch, nil
}

// validateObservedTips validates immutable IDs from one ref observation. Bodies,
// parsed checkpoints, and dependency types are shared only within this call.
func (r *Repository) validateObservedTips(ctx context.Context, refs map[string]string, names []string) (map[string]Checkpoint, error) {
	result := make(map[string]Checkpoint, len(names))
	if len(names) == 0 {
		return result, nil
	}
	length, err := r.objectIDLength(ctx)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	seen := make(map[string]bool)
	for _, name := range names {
		oid := refs[name]
		if !validObjectID(oid, length) {
			return nil, invalidCheckpoint("checkpoint ref must point directly to a commit")
		}
		if !seen[oid] {
			seen[oid] = true
			ids = append(ids, oid)
		}
	}
	objects, err := r.batchObjects(ctx, ids, true)
	if err != nil {
		return nil, err
	}
	parsed := make(map[string]Checkpoint)
	parseErrors := make(map[string]error)
	parse := func(oid string) {
		if _, found := parsed[oid]; found || parseErrors[oid] != nil {
			return
		}
		object := objects[oid]
		if object.kind != "commit" {
			parseErrors[oid] = invalidCheckpoint("checkpoint commit is unavailable or is not a direct commit")
			return
		}
		cp, err := parseDiscoveryCheckpoint(oid, object.raw, length)
		if err != nil {
			parseErrors[oid] = err
			return
		}
		parsed[oid] = cp
	}
	parents := []string{}
	for _, oid := range ids {
		parse(oid)
		cp, valid := parsed[oid]
		if valid && cp.Parent != cp.Base && !seen[cp.Parent] {
			seen[cp.Parent] = true
			parents = append(parents, cp.Parent)
		}
	}
	if len(parents) > 0 {
		more, err := r.batchObjects(ctx, parents, true)
		if err != nil {
			return nil, err
		}
		for oid, object := range more {
			objects[oid] = object
		}
	}
	// Parse parents even when their body was already read as another ref's tip.
	for _, oid := range ids {
		if cp, valid := parsed[oid]; valid && cp.Parent != cp.Base {
			parse(cp.Parent)
		}
	}
	dependencies := []string{}
	for _, cp := range parsed {
		for _, oid := range []string{cp.Base, cp.Parent} {
			if !seen[oid] {
				seen[oid] = true
				dependencies = append(dependencies, oid)
			}
		}
	}
	if len(dependencies) > 0 {
		more, err := r.batchObjects(ctx, dependencies, false)
		if err != nil {
			return nil, err
		}
		for oid, object := range more {
			objects[oid] = object
		}
	}
	// cat-file's type check does not parse a commit's Git headers. Ask Git to
	// parse every available required commit in one nontraversing operation, just
	// as the ordinary reader's revision resolution does for each dependency.
	commits := make([]string, 0, len(objects))
	for oid, object := range objects {
		if object.kind == "commit" {
			commits = append(commits, oid)
		}
	}
	if len(commits) > 0 {
		if _, err := r.gitEnv(ctx, nil, objectBatchInput(commits), "rev-list", "--no-walk", "--stdin", "--"); err != nil {
			if ctx.Err() != nil || outputLimitError(err) {
				return nil, err
			}
			return nil, &Error{Kind: KindInvalidRef, Op: "ref", Message: "invalid checkpoint commit or dependency", Err: err}
		}
	}
	validateDependencies := func(cp Checkpoint) error {
		if objects[cp.Base].kind != "commit" {
			return invalidCheckpoint("checkpoint base commit is unavailable")
		}
		if objects[cp.Parent].kind != "commit" {
			return invalidCheckpoint("checkpoint parent commit is unavailable")
		}
		return nil
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, &Error{Kind: KindGit, Op: "ref", Message: "operation cancelled", Err: err}
		}
		oid := refs[name]
		if err := parseErrors[oid]; err != nil {
			return nil, &Error{Kind: KindInvalidRef, Op: "ref", Message: "invalid checkpoint ref target", Err: err}
		}
		cp := parsed[oid]
		if err := validateDependencies(cp); err != nil {
			return nil, &Error{Kind: KindInvalidRef, Op: "ref", Message: "invalid checkpoint ref target", Err: err}
		}
		branch, err := checkpointRefBranch(name)
		if err != nil {
			return nil, err
		}
		if cp.Branch != branch {
			return nil, &Error{Kind: KindInvalidRef, Op: "ref", Message: "checkpoint metadata does not match its branch"}
		}
		if cp.Parent != cp.Base {
			if err := parseErrors[cp.Parent]; err != nil {
				return nil, err
			}
			parent := parsed[cp.Parent]
			if err := validateDependencies(parent); err != nil {
				return nil, err
			}
			if parent.Branch != cp.Branch || parent.Base != cp.Base {
				return nil, &Error{Kind: KindInvalidRef, Op: "ref", Message: "checkpoint parent belongs to another stack"}
			}
		}
		result[name] = cp
	}
	return result, nil
}
