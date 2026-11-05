package wip

import (
	"context"
	"sort"
	"strconv"
	"strings"
)

// StackDiscoveryOptions selects direct endpoint roots, or resumes an earlier
// result. CheckpointLimit bounds additional ancestry candidates, not root
// classification. It must be positive. Roots and Continuation are exclusive.
type StackDiscoveryOptions struct {
	Roots           []string           `json:"roots,omitempty"`
	CheckpointLimit int                `json:"checkpoint_limit"`
	Continuation    *StackContinuation `json:"-"`
}

// RootClassification records endpoint ownership, not ordinary-history provenance.
// Kind is checkpoint, noncheckpoint, unavailable, or invalid.
type RootClassification struct {
	Commit string `json:"commit"`
	Kind   string `json:"kind"`
}

// StackDiscoveryCoverage separates bounded progress from integrity failures.
// Stop is complete, budget, failure, or state_limit. Exhausted means no known
// frontier remains; Failures must still be checked for blocked ancestry.
type StackDiscoveryCoverage struct {
	RootsComplete bool   `json:"roots_complete"`
	Exhausted     bool   `json:"exhausted"`
	Stop          string `json:"stop"`
}

// StackDiscoveryResult publishes each checkpoint once across continuation pages.
// Visited counts additional ancestry candidates, excluding roots and dependency
// lookahead. Root checkpoints are returned even when they exceed the page budget.
// Roots contains classifications only on the initial call. A state_limit stop
// has no continuation; callers must narrow scope or refresh with fewer roots.
type StackDiscoveryResult struct {
	Roots        []RootClassification   `json:"roots"`
	Checkpoints  []Checkpoint           `json:"checkpoints"`
	Visited      int                    `json:"visited"`
	Failures     []DiscoveryFailure     `json:"failures"`
	Coverage     StackDiscoveryCoverage `json:"coverage"`
	Continuation *StackContinuation     `json:"-"`
}

// StackContinuation is an immutable, process-local cursor bound to the exact
// working tree. Reusing a cursor produces the same page while objects exist.
// Its state is opaque and cannot be serialized into an authoritative cursor.
type StackContinuation struct {
	root, gitDir string
	length       int
	expanded     map[string]bool
	frontier     []stackFrontier
	failed       bool
}

type stackFrontier struct{ commit, branch, base string }

const stackRootLimit = 4096
const stackStateLimit = 16384

// DiscoverStacks classifies roots before following validated checkpoint stacks.
// Ordinary roots are classified but never traversed. Each base-specific walk
// stops at that stack's base and only publishes checkpoints reachable through
// validated links; malformed intermediary commits cannot expose older entries.
// At most 4,096 distinct roots and 16,384 cursor entries are retained; exceeding
// either bound returns an explicit state_limit stop. General ancestry discovery
// remains available through Discover.
func (r *Repository) DiscoverStacks(ctx context.Context, options StackDiscoveryOptions) (StackDiscoveryResult, error) {
	result := StackDiscoveryResult{Roots: []RootClassification{}, Checkpoints: []Checkpoint{}, Failures: []DiscoveryFailure{}}
	stop := func(err error) (StackDiscoveryResult, error) {
		result.Coverage.Stop = "failure"
		result.Continuation = nil
		return result, err
	}
	invalid := func(message string) (StackDiscoveryResult, error) {
		return stop(&Error{Kind: KindInvalidOption, Op: "discover stacks", Message: message})
	}
	if r == nil || r.root == "" || r.gitPath == "" {
		return invalid("repository must be opened first")
	}
	if options.CheckpointLimit <= 0 || options.CheckpointLimit == int(^uint(0)>>1) {
		return invalid("checkpoint limit must be positive and permit one lookahead")
	}
	if options.Continuation != nil && len(options.Roots) != 0 {
		return invalid("roots and continuation are mutually exclusive")
	}
	if err := ctx.Err(); err != nil {
		return stop(&Error{Kind: KindGit, Op: "discover stacks", Message: "operation cancelled", Err: err})
	}
	state := StackContinuation{root: r.root, gitDir: r.gitDir, expanded: map[string]bool{}}
	rootLimited := false
	if previous := options.Continuation; previous != nil {
		if previous.root != r.root || previous.gitDir != r.gitDir || previous.length == 0 || previous.expanded == nil {
			return invalid("continuation does not belong to this repository")
		}
		state.length, state.failed = previous.length, previous.failed
		for oid, value := range previous.expanded {
			state.expanded[oid] = value
		}
		state.frontier = append([]stackFrontier(nil), previous.frontier...)
		result.Coverage.RootsComplete = true
	} else {
		if len(options.Roots) == 0 {
			result.Coverage = StackDiscoveryCoverage{RootsComplete: true, Exhausted: true, Stop: "complete"}
			return result, nil
		}
		length, err := r.objectIDLength(ctx)
		if err != nil {
			return stop(err)
		}
		state.length = length
		roots := []string{}
		seen := map[string]bool{}
		for _, oid := range options.Roots {
			if !validObjectID(oid, length) {
				return invalid("roots must be full direct commit object IDs")
			}
			if !seen[oid] {
				if len(roots) < stackRootLimit {
					seen[oid] = true
					roots = append(roots, oid)
				} else {
					rootLimited = true
				}
			}
		}
		validated, err := r.validateStackCandidates(ctx, roots, length)
		if err != nil {
			return stop(err)
		}
		known := map[string]Checkpoint{}
		for _, oid := range roots {
			item := validated[oid]
			result.Roots = append(result.Roots, RootClassification{Commit: oid, Kind: item.kind})
			state.expanded[oid] = true
			if item.err != nil {
				state.failed = true
				result.Failures = append(result.Failures, DiscoveryFailure{Commit: oid, Message: item.err.Error(), Err: item.err})
			}
			if item.kind == "checkpoint" {
				known[oid] = item.checkpoint
				result.Checkpoints = append(result.Checkpoints, item.checkpoint)
			}
		}
		// Collapse chains already supplied as roots without charging ancestry work.
		for _, oid := range roots {
			cp, ok := known[oid]
			if !ok {
				continue
			}
			if cp.Parent != cp.Base && !state.expanded[cp.Parent] {
				state.frontier = append(state.frontier, stackFrontier{cp.Parent, cp.Branch, cp.Base})
			}
		}
		result.Coverage.RootsComplete = !rootLimited
	}
	fail := func(oid string, err error) {
		state.failed = true
		result.Failures = append(result.Failures, DiscoveryFailure{Commit: oid, Message: err.Error(), Err: err})
	}
	normalize := func() {
		sort.Slice(state.frontier, func(i, j int) bool {
			a, b := state.frontier[i], state.frontier[j]
			if a.base != b.base {
				return a.base < b.base
			}
			if a.branch != b.branch {
				return a.branch < b.branch
			}
			return a.commit < b.commit
		})
		unique := state.frontier[:0]
		for _, entry := range state.frontier {
			if state.expanded[entry.commit] {
				continue
			}
			if len(unique) == 0 || unique[len(unique)-1] != entry {
				unique = append(unique, entry)
			}
		}
		state.frontier = unique
	}
	normalize()
	visited := map[string]bool{}
	for len(state.frontier) > 0 && result.Visited < options.CheckpointLimit && !rootLimited {
		if len(state.expanded)+len(state.frontier) >= stackStateLimit {
			break
		}
		base := state.frontier[0].base
		group := []stackFrontier{}
		for _, entry := range state.frontier {
			if entry.base == base {
				group = append(group, entry)
			}
		}
		count := options.CheckpointLimit - result.Visited
		if capacity := stackStateLimit - len(state.expanded) - len(state.frontier); count > capacity {
			count = capacity
		}
		inputs := []string{}
		inputSeen := map[string]bool{}
		for _, entry := range group {
			if !inputSeen[entry.commit] {
				inputs = append(inputs, entry.commit)
				inputSeen[entry.commit] = true
			}
		}
		inputs = append(inputs, "^"+base)
		out, err := r.gitEnv(ctx, nil, objectBatchInput(inputs), "rev-list", "--first-parent", "--topo-order", "--max-count="+strconv.Itoa(count+1), "--stdin", "--")
		if err != nil {
			return stop(err)
		}
		ids := strings.Fields(string(out))
		for _, oid := range ids {
			if !validObjectID(oid, state.length) {
				return stop(&Error{Kind: KindGit, Op: "discover stacks", Message: "invalid traversal object ID"})
			}
		}
		if len(ids) > count {
			ids = ids[:count]
		}
		items, err := r.validateStackCandidates(ctx, ids, state.length)
		if err != nil {
			return stop(err)
		}
		available := make(map[string]bool, len(ids))
		for _, oid := range ids {
			available[oid] = true
			if !state.expanded[oid] && !visited[oid] {
				visited[oid] = true
				result.Visited++
			}
		}
		next := []stackFrontier{}
		for _, entry := range state.frontier {
			if entry.base != base {
				next = append(next, entry)
			}
		}
		queue := append([]stackFrontier(nil), group...)
		// Walk only validated parent links in the bounded body batch. Git's traversal
		// may contain older commits behind an invalid node; those remain unpublished.
		for len(queue) > 0 {
			entry := queue[0]
			queue = queue[1:]
			if state.expanded[entry.commit] {
				continue
			}
			if !available[entry.commit] {
				next = append(next, entry)
				continue
			}
			item := items[entry.commit]
			state.expanded[entry.commit] = true
			if item.err != nil {
				fail(entry.commit, item.err)
				continue
			}
			cp := item.checkpoint
			if item.kind != "checkpoint" {
				fail(entry.commit, invalidCheckpoint("checkpoint parent is not an owned checkpoint"))
				continue
			}
			if cp.Branch != entry.branch || cp.Base != entry.base {
				fail(entry.commit, invalidCheckpoint("checkpoint parent belongs to another stack"))
				continue
			}
			result.Checkpoints = append(result.Checkpoints, cp)
			if cp.Parent != cp.Base && !state.expanded[cp.Parent] {
				queue = append(queue, stackFrontier{cp.Parent, cp.Branch, cp.Base})
			}
		}
		state.frontier = next
		normalize()
		// An excluded or unavailable frontier must not cause a zero-progress loop.
		if len(ids) == 0 {
			for _, entry := range group {
				fail(entry.commit, invalidCheckpoint("checkpoint frontier is outside its stack"))
				state.expanded[entry.commit] = true
			}
			normalize()
		}
	}
	result.Coverage.Exhausted = len(state.frontier) == 0 && !rootLimited
	switch {
	case rootLimited || len(state.frontier) > 0 && len(state.expanded)+len(state.frontier) >= stackStateLimit:
		result.Coverage.Stop = "state_limit"
	case len(state.frontier) > 0:
		result.Coverage.Stop = "budget"
		result.Continuation = &state
	case state.failed:
		result.Coverage.Stop = "failure"
	default:
		result.Coverage.Stop = "complete"
	}
	return result, nil
}

type stackCandidate struct {
	kind       string
	checkpoint Checkpoint
	err        error
}

// Validate candidates and their immediate parent storage using at most three
// object batches. Dependencies are metadata only; ordinary trees/blobs are not
// loaded and parents outside the page are not published as visited candidates.
func (r *Repository) validateStackCandidates(ctx context.Context, ids []string, length int) (map[string]stackCandidate, error) {
	objects, err := r.batchObjects(ctx, ids, true)
	if err != nil {
		return nil, err
	}
	result := make(map[string]stackCandidate, len(ids))
	parsed := map[string]Checkpoint{}
	parents := []string{}
	seen := map[string]bool{}
	for _, oid := range ids {
		object := objects[oid]
		item := stackCandidate{kind: "noncheckpoint"}
		switch {
		case object.kind != "commit":
			item.kind = "unavailable"
			item.err = &Error{Kind: KindNotFound, Op: "discover stacks", Message: "root or candidate is not an available direct commit"}
		case checkpointCandidate(object.raw):
			cp, err := parseDiscoveryCheckpoint(oid, object.raw, length)
			if err != nil {
				item.kind = "invalid"
				item.err = err
			} else {
				item.kind = "checkpoint"
				item.checkpoint = cp
				parsed[oid] = cp
				if cp.Parent != cp.Base && objects[cp.Parent].raw == nil && !seen[cp.Parent] {
					seen[cp.Parent] = true
					parents = append(parents, cp.Parent)
				}
			}
		}
		result[oid] = item
	}
	if len(parents) > 0 {
		more, err := r.batchObjects(ctx, parents, true)
		if err != nil {
			return nil, err
		}
		for oid, obj := range more {
			objects[oid] = obj
		}
	}
	dependencies := []string{}
	seen = map[string]bool{}
	need := func(oid string) {
		if _, ok := objects[oid]; !ok && !seen[oid] {
			seen[oid] = true
			dependencies = append(dependencies, oid)
		}
	}
	parentErrors := map[string]error{}
	for _, oid := range ids {
		cp, ok := parsed[oid]
		if !ok {
			continue
		}
		need(cp.Base)
		need(cp.Parent)
		if cp.Parent == cp.Base {
			continue
		}
		if _, ok := parsed[cp.Parent]; !ok {
			object := objects[cp.Parent]
			parent, err := parseDiscoveryCheckpoint(cp.Parent, object.raw, length)
			if object.kind != "commit" {
				err = invalidCheckpoint("checkpoint parent commit is unavailable")
			}
			if err != nil {
				parentErrors[cp.Parent] = err
				continue
			}
			parsed[cp.Parent] = parent
		}
		parent := parsed[cp.Parent]
		need(parent.Base)
		need(parent.Parent)
	}
	if len(dependencies) > 0 {
		more, err := r.batchObjects(ctx, dependencies, false)
		if err != nil {
			return nil, err
		}
		for oid, obj := range more {
			objects[oid] = obj
		}
	}
	for _, oid := range ids {
		item := result[oid]
		if item.kind != "checkpoint" {
			continue
		}
		cp := item.checkpoint
		var invalid error
		if objects[cp.Base].kind != "commit" || objects[cp.Parent].kind != "commit" {
			invalid = invalidCheckpoint("checkpoint base or parent commit is unavailable")
		} else if cp.Parent != cp.Base {
			parent := parsed[cp.Parent]
			switch {
			case parentErrors[cp.Parent] != nil:
				invalid = parentErrors[cp.Parent]
			case parent.Branch != cp.Branch || parent.Base != cp.Base:
				invalid = invalidCheckpoint("checkpoint parent belongs to another stack")
			case objects[parent.Base].kind != "commit" || objects[parent.Parent].kind != "commit":
				invalid = invalidCheckpoint("checkpoint parent has an unavailable base or parent commit")
			}
		}
		if invalid != nil {
			item.kind = "invalid"
			item.err = invalid
			result[oid] = item
		}
	}
	return result, nil
}
