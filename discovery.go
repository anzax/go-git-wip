package wip

import (
	"bytes"
	"context"
	"strconv"
	"strings"
)

// Discover walks explicit roots once, counting every candidate commit against a
// total budget. It reads and validates checkpoint storage in batches without
// comparing paths. Invalid owned candidates and unavailable roots are explicit
// failures; command errors return a partial, incomplete result and an error.
func (r *Repository) Discover(ctx context.Context, options DiscoveryOptions) (DiscoveryResult, error) {
	result := DiscoveryResult{Checkpoints: []Checkpoint{}, Failures: []DiscoveryFailure{}}
	fail := func(oid string, err error) {
		result.Incomplete = true
		result.Failures = append(result.Failures, DiscoveryFailure{Commit: oid, Message: err.Error(), Err: err})
	}
	stop := func(err error) (DiscoveryResult, error) {
		result.Incomplete = true
		return result, err
	}
	if r == nil || r.root == "" || r.gitPath == "" {
		return stop(&Error{Kind: KindInvalidOption, Op: "discover", Message: "repository must be opened first"})
	}
	if options.CandidateLimit <= 0 || options.CandidateLimit == int(^uint(0)>>1) {
		return stop(&Error{Kind: KindInvalidOption, Op: "discover", Message: "candidate limit must be positive and permit one lookahead"})
	}
	if err := ctx.Err(); err != nil {
		return stop(&Error{Kind: KindGit, Op: "discover", Message: "operation cancelled", Err: err})
	}
	if len(options.Roots) == 0 {
		return result, nil
	}
	length, err := r.objectIDLength(ctx)
	if err != nil {
		return stop(err)
	}
	roots := make([]string, 0, len(options.Roots))
	seen := make(map[string]bool, len(options.Roots))
	for _, oid := range options.Roots {
		if !validObjectID(oid, length) {
			return stop(&Error{Kind: KindInvalidOption, Op: "discover", Message: "roots must be full direct commit object IDs"})
		}
		if !seen[oid] {
			seen[oid] = true
			roots = append(roots, oid)
		}
	}
	metadata, err := r.batchObjects(ctx, roots, false)
	if err != nil {
		return stop(err)
	}
	validRoots := make([]string, 0, len(roots))
	for _, oid := range roots {
		if metadata[oid].kind != "commit" {
			fail(oid, &Error{Kind: KindNotFound, Op: "discover", Message: "root is not an available direct commit"})
			continue
		}
		validRoots = append(validRoots, oid)
	}
	if len(validRoots) == 0 {
		return result, nil
	}
	// A one-ID lookahead distinguishes a full window from an exhausted walk.
	// No --all, path filters, or caller revisions can enlarge or alter the roots.
	out, err := r.gitEnv(ctx, nil, objectBatchInput(validRoots), "rev-list", "--max-count="+strconv.Itoa(options.CandidateLimit+1), "--stdin", "--")
	if err != nil {
		return stop(err)
	}
	ids := strings.Fields(string(out))
	for _, oid := range ids {
		if !validObjectID(oid, length) {
			return stop(&Error{Kind: KindGit, Op: "discover", Message: "invalid traversal object ID"})
		}
	}
	if len(ids) > options.CandidateLimit {
		result.Incomplete = true
		ids = ids[:options.CandidateLimit]
	}
	result.Visited = len(ids)
	objects, err := r.batchObjects(ctx, ids, true)
	if err != nil {
		return stop(err)
	}
	for oid, object := range objects {
		metadata[oid] = object
	}
	checkpoints := make([]Checkpoint, 0, len(ids))
	parsed := make(map[string]Checkpoint)
	parseErrors := make(map[string]error)
	parents := []string{}
	parentSeen := make(map[string]bool)
	for _, oid := range ids {
		object := objects[oid]
		if object.kind != "commit" {
			fail(oid, &Error{Kind: KindNotFound, Op: "discover", Message: "candidate commit became unavailable"})
			continue
		}
		if !checkpointCandidate(object.raw) {
			continue
		}
		cp, err := parseDiscoveryCheckpoint(oid, object.raw, length)
		if err != nil {
			parseErrors[oid] = err
			fail(oid, err)
			continue
		}
		parsed[oid] = cp
		checkpoints = append(checkpoints, cp)
		if cp.Parent != cp.Base && objects[cp.Parent].raw == nil && !parentSeen[cp.Parent] {
			parentSeen[cp.Parent] = true
			parents = append(parents, cp.Parent)
		}
	}
	// Reuse candidate bodies, reading only checkpoint parents outside the window.
	if len(parents) > 0 {
		more, err := r.batchObjects(ctx, parents, true)
		if err != nil {
			return stop(err)
		}
		for oid, object := range more {
			objects[oid], metadata[oid] = object, object
		}
	}
	dependencies := []string{}
	dependencySeen := make(map[string]bool)
	need := func(oid string) {
		if _, known := metadata[oid]; !known && !dependencySeen[oid] {
			dependencySeen[oid] = true
			dependencies = append(dependencies, oid)
		}
	}
	for _, cp := range checkpoints {
		need(cp.Base)
		need(cp.Parent)
		if cp.Parent == cp.Base {
			continue
		}
		if _, found := parsed[cp.Parent]; found || parseErrors[cp.Parent] != nil {
			continue
		}
		object := objects[cp.Parent]
		if object.kind != "commit" {
			parseErrors[cp.Parent] = invalidCheckpoint("checkpoint parent commit is unavailable")
			continue
		}
		parent, err := parseDiscoveryCheckpoint(cp.Parent, object.raw, length)
		if err != nil {
			parseErrors[cp.Parent] = err
			continue
		}
		parsed[cp.Parent] = parent
		need(parent.Base)
		need(parent.Parent)
	}
	if len(dependencies) > 0 {
		more, err := r.batchObjects(ctx, dependencies, false)
		if err != nil {
			return stop(err)
		}
		for oid, object := range more {
			metadata[oid] = object
		}
	}
	for _, cp := range checkpoints {
		if err := ctx.Err(); err != nil {
			return stop(&Error{Kind: KindGit, Op: "discover", Message: "operation cancelled", Err: err})
		}
		if metadata[cp.Base].kind != "commit" || metadata[cp.Parent].kind != "commit" {
			fail(cp.Commit, invalidCheckpoint("checkpoint base or parent commit is unavailable"))
			continue
		}
		if cp.Parent != cp.Base {
			if err := parseErrors[cp.Parent]; err != nil {
				fail(cp.Commit, err)
				continue
			}
			parent := parsed[cp.Parent]
			if parent.Branch != cp.Branch || parent.Base != cp.Base {
				fail(cp.Commit, invalidCheckpoint("checkpoint parent belongs to another stack"))
				continue
			}
			if metadata[parent.Base].kind != "commit" || metadata[parent.Parent].kind != "commit" {
				fail(cp.Commit, invalidCheckpoint("checkpoint parent has an unavailable base or parent commit"))
				continue
			}
		}
		result.Checkpoints = append(result.Checkpoints, cp)
	}
	return result, nil
}

func checkpointCandidate(raw []byte) bool {
	_, body, found := bytes.Cut(raw, []byte("\n\n"))
	if !found {
		return false
	}
	if bytes.HasPrefix(body, []byte(checkpointNamespace)) {
		return true
	}
	last := bytes.TrimRight(body, "\n")
	if i := bytes.LastIndexByte(last, '\n'); i >= 0 {
		last = last[i+1:]
	}
	return bytes.HasPrefix(last, []byte(checkpointNamespace))
}

func parseDiscoveryCheckpoint(oid string, raw []byte, length int) (Checkpoint, error) {
	cp, err := parseCheckpoint(oid, raw)
	if err != nil {
		return Checkpoint{}, err
	}
	if !validCheckpointBranch(cp.Branch) {
		return Checkpoint{}, invalidCheckpoint("invalid checkpoint branch name")
	}
	if !validObjectID(cp.Base, length) || !validObjectID(cp.Parent, length) {
		return Checkpoint{}, invalidCheckpoint("checkpoint base and parent must be full direct commit IDs")
	}
	return cp, nil
}

// Validate the complete refs/wip/<branch> name using the documented default
// check-ref-format rules, without starting a process for each candidate.
// https://git-scm.com/docs/git-check-ref-format
func validCheckpointBranch(branch string) bool {
	if branch == "" || strings.HasSuffix(branch, ".") || strings.Contains(branch, "..") || strings.Contains(branch, "@{") {
		return false
	}
	for _, component := range strings.Split(branch, "/") {
		if component == "" || strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	for i := 0; i < len(branch); i++ {
		if branch[i] <= ' ' || branch[i] == 127 || strings.ContainsRune("~^:?*[\\", rune(branch[i])) {
			return false
		}
	}
	return true
}

type batchObject struct {
	kind string
	raw  []byte
}

func objectBatchInput(ids []string) []byte {
	return []byte(strings.Join(ids, "\n") + "\n")
}

// All inputs are validated full IDs, so the newline batch protocol cannot accept
// expressions, options, or extra records. Body framing uses sizes, never lines.
func (r *Repository) batchObjects(ctx context.Context, ids []string, contents bool) (map[string]batchObject, error) {
	result := make(map[string]batchObject, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	mode := "--batch-check"
	if contents {
		mode = "--batch"
	}
	out, err := r.gitEnv(ctx, nil, objectBatchInput(ids), "cat-file", mode)
	if err != nil {
		return nil, err
	}
	invalid := func() (map[string]batchObject, error) {
		return nil, &Error{Kind: KindGit, Op: "discover", Message: "invalid Git object batch framing"}
	}
	for _, oid := range ids {
		if err := ctx.Err(); err != nil {
			return nil, &Error{Kind: KindGit, Op: "discover", Message: "operation cancelled", Err: err}
		}
		line, rest, found := bytes.Cut(out, []byte{'\n'})
		if !found {
			return invalid()
		}
		out = rest
		fields := strings.Fields(string(line))
		if len(fields) == 2 && fields[0] == oid && fields[1] == "missing" {
			result[oid] = batchObject{}
			continue
		}
		if len(fields) != 3 || fields[0] != oid {
			return invalid()
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || size < 0 {
			return invalid()
		}
		object := batchObject{kind: fields[1]}
		if contents {
			if size >= int64(len(out)) || out[int(size)] != '\n' {
				return invalid()
			}
			object.raw, out = out[:int(size)], out[int(size)+1:]
		}
		result[oid] = object
	}
	if len(out) != 0 {
		return invalid()
	}
	return result, nil
}
