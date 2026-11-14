package wip

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PruneOptions selects one branch's retained checkpoint history. Either positive
// retention rule protects a checkpoint; the live tip always survives. Zero omits
// a rule. Ref defaults to the current attached branch's refs/wip/<branch>.
type PruneOptions struct {
	Ref      string `json:"ref,omitempty"`
	KeepLast int    `json:"keep_last"`
	KeepDays int    `json:"keep_days"`
	DryRun   bool   `json:"dry_run"`
}

// PruneResult reports proposed counts on dry runs and completed publication on
// writes. Tip remains PreviousTip on dry runs. Publication is two ordered renames:
// inspect both flags and the repository after any error, including cancellation.
type PruneResult struct {
	Ref             string `json:"ref"`
	Before          int    `json:"before"`
	Kept            int    `json:"kept"`
	Pruned          int    `json:"pruned"`
	Rewritten       int    `json:"rewritten"`
	PreviousTip     string `json:"previous_tip"`
	Tip             string `json:"tip"`
	DryRun          bool   `json:"dry_run"`
	ReflogPublished bool   `json:"reflog_published"`
	RefPublished    bool   `json:"ref_published"`
}

type pruneCommit struct {
	checkpoint Checkpoint
	raw        []byte
}
type pruneLogEntry struct{ old, new, suffix string }
type pruneSnapshot struct {
	tip     string
	log     []byte
	entries []pruneLogEntry
}
type prunePaths struct{ common, ref, log string }

// Prune removes checkpoint discovery roots and rewrites retained ancestry, without
// changing normal refs, HEAD, index or worktree. It never runs object GC. Only the
// files ref backend is supported. Objects can remain after a failed publication.
func (r *Repository) Prune(ctx context.Context, options PruneOptions) (PruneResult, error) {
	result := PruneResult{Ref: options.Ref, DryRun: options.DryRun}
	if r == nil || r.root == "" || r.gitPath == "" {
		return result, &Error{Kind: KindInvalidOption, Op: "prune", Message: "repository must be opened first"}
	}
	if options.KeepLast < 0 || options.KeepDays < 0 || options.KeepLast == 0 && options.KeepDays == 0 || uint64(options.KeepDays) > uint64(math.MaxInt64/int64(24*time.Hour)) {
		return result, &Error{Kind: KindInvalidOption, Op: "prune", Message: "supply positive keep-last and/or keep-days within the supported duration range"}
	}
	now := time.Now()
	if options.Ref == "" {
		info, err := r.Info(ctx)
		if err != nil {
			return result, err
		}
		if info.Branch == "" {
			return result, &Error{Kind: KindUnsupported, Op: "prune", Message: "detached HEAD requires an explicit checkpoint ref"}
		}
		options.Ref = checkpointRef(info.Branch)
		result.Ref = options.Ref
	}
	branch, err := r.validateCheckpointRef(ctx, options.Ref)
	if err != nil {
		return result, err
	}
	paths, err := r.prunePaths(ctx, options.Ref)
	if err != nil {
		return result, err
	}
	length, err := r.objectIDLength(ctx)
	if err != nil {
		return result, err
	}
	snapshot, err := r.readPruneSnapshot(ctx, paths, options.Ref, length)
	if err != nil {
		return result, err
	}
	result.PreviousTip, result.Tip = snapshot.tip, snapshot.tip
	if snapshot.tip == "" {
		return result, nil
	}
	commits, rank, err := r.pruneInventory(ctx, snapshot, branch, length)
	if err != nil {
		return result, err
	}
	result.Before = len(commits)
	keep := map[string]bool{snapshot.tip: true}
	cutoff := now.Add(-time.Duration(options.KeepDays) * 24 * time.Hour)
	for i, id := range rank {
		if options.KeepLast > 0 && i < options.KeepLast || options.KeepDays > 0 && !commits[id].checkpoint.Time.Before(cutoff) {
			keep[id] = true
		}
	}
	result.Kept = len(keep)
	result.Pruned = result.Before - result.Kept
	// Determine changed parents before creating objects, including transitive rewrites.
	parents := map[string]string{}
	changed := map[string]bool{}
	order := pruneParentOrder(commits)
	for _, id := range order {
		if !keep[id] {
			continue
		}
		cp := commits[id].checkpoint
		parent := cp.Parent
		for parent != cp.Base && !keep[parent] {
			parent = commits[parent].checkpoint.Parent
		}
		parents[id] = parent
		if parent != cp.Parent || changed[parent] {
			headers, _, _ := bytes.Cut(commits[id].raw, []byte("\n\n"))
			for _, line := range bytes.Split(headers, []byte("\n")) {
				if bytes.HasPrefix(line, []byte("gpgsig ")) || bytes.HasPrefix(line, []byte("gpgsig-sha256 ")) {
					return result, &Error{Kind: KindUnsupported, Op: "prune", Message: "cannot reparent a signed checkpoint"}
				}
			}
			changed[id] = true
			result.Rewritten++
		}
	}
	if result.Pruned == 0 {
		return result, nil
	}
	// Canonical Git object hashing permits a complete rewrite plan without object
	// creation. Exact-byte preservation cannot represent two retained events if
	// reparenting makes their commit objects identical; reject that plan safely.
	ids := map[string]string{}
	planned := map[string][]byte{}
	targets := map[string]bool{}
	for _, id := range order {
		if !keep[id] {
			continue
		}
		target := id
		if changed[id] {
			parent := parents[id]
			if rewritten, ok := ids[parent]; ok {
				parent = rewritten
			}
			raw := bytes.Replace(commits[id].raw, []byte("\nparent "+commits[id].checkpoint.Parent+"\n"), []byte("\nparent "+parent+"\n"), 1)
			target = pruneObjectID(raw, length)
			planned[id] = raw
		}
		if targets[target] {
			return result, &Error{Kind: KindUnsupported, Op: "prune", Message: "retention would collapse distinct retained checkpoints into one commit"}
		}
		targets[target] = true
		ids[id] = target
	}
	log, err := pruneReflog(snapshot, commits, keep, parents, ids, length)
	if err != nil {
		return result, err
	}
	if options.DryRun {
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, pruneIO("operation cancelled", err)
	}
	// Native ref lock excludes update-ref, capture and delete on this owned ref.
	refMode, err := prunePublicationMode(paths, paths.ref)
	if err != nil {
		return result, err
	}
	logMode, err := prunePublicationMode(paths, paths.log)
	if err != nil {
		return result, err
	}
	refLock, err := pruneLock(paths.common, paths.ref+".lock", refMode)
	if err != nil {
		return result, err
	}
	refOwned := true
	defer func() {
		_ = refLock.Close()
		if refOwned {
			_ = os.Remove(paths.ref + ".lock")
		}
	}()
	current, err := r.readPruneSnapshot(ctx, paths, options.Ref, length)
	if err != nil {
		return result, err
	}
	if current.tip != snapshot.tip || !bytes.Equal(current.log, snapshot.log) {
		return result, &Error{Kind: KindConflict, Op: "prune", Message: "checkpoint ref or reflog changed during pruning"}
	}
	logLock, err := pruneLock(paths.common, paths.log+".lock", logMode)
	if err != nil {
		return result, err
	}
	logOwned := true
	defer func() {
		_ = logLock.Close()
		if logOwned {
			_ = os.Remove(paths.log + ".lock")
		}
	}()
	for _, id := range order {
		raw, ok := planned[id]
		if !ok {
			continue
		}
		out, err := r.gitEnv(ctx, nil, raw, "hash-object", "-t", "commit", "-w", "--stdin")
		if err != nil {
			return result, err
		}
		if strings.TrimSpace(string(out)) != ids[id] {
			return result, pruneIO("Git object ID differs from planned checkpoint", nil)
		}
	}
	if err := stagePruneFile(logLock, log); err != nil {
		return result, err
	}
	if err := stagePruneFile(refLock, []byte(ids[snapshot.tip]+"\n")); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, pruneIO("operation cancelled before publication", err)
	}
	// After publication begins, finish the bounded local rename sequence even when
	// cancelled. A log-first partial outcome keeps new historical roots discoverable.
	if err := os.Rename(paths.log+".lock", paths.log); err != nil {
		return result, pruneIO("publish checkpoint reflog", err)
	}
	logOwned = false
	result.ReflogPublished = true
	if err := syncPruneDirectory(filepath.Dir(paths.log)); err != nil {
		return result, err
	}
	if err := os.Rename(paths.ref+".lock", paths.ref); err != nil {
		return result, pruneIO("publish checkpoint ref; inspect partial reflog publication", err)
	}
	refOwned = false
	result.RefPublished = true
	result.Tip = ids[snapshot.tip]
	if err := syncPruneDirectory(filepath.Dir(paths.ref)); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, pruneIO("operation cancelled after publication; inspect result", err)
	}
	return result, nil
}

func pruneIO(message string, err error) error {
	return &Error{Kind: KindGit, Op: "prune", Message: message, Err: err}
}

func (r *Repository) prunePaths(ctx context.Context, ref string) (prunePaths, error) {
	out, err := r.git(ctx, "config", "--get", "extensions.refStorage")
	if err != nil && !gitExitCode(err, 1) {
		return prunePaths{}, err
	}
	backend := strings.TrimSpace(string(out))
	if backend != "" && backend != "files" {
		return prunePaths{}, &Error{Kind: KindUnsupported, Op: "prune", Message: "pruning requires the files ref backend"}
	}
	out, err = r.git(ctx, "rev-parse", "--git-common-dir")
	if err != nil {
		return prunePaths{}, err
	}
	common := strings.TrimSuffix(string(out), "\n")
	if !filepath.IsAbs(common) {
		common = filepath.Join(r.root, common)
	}
	common, err = filepath.Abs(common)
	if err != nil {
		return prunePaths{}, pruneIO("resolve Git common directory", err)
	}
	canonical, err := filepath.EvalSymlinks(common)
	if err != nil {
		return prunePaths{}, pruneIO("resolve Git common directory", err)
	}
	paths := prunePaths{common: canonical}
	for _, item := range []struct {
		name string
		dst  *string
	}{{ref, &paths.ref}, {"logs/" + ref, &paths.log}, {"shallow", nil}, {"info/grafts", nil}} {
		out, err = r.git(ctx, "rev-parse", "--git-path", item.name)
		if err != nil {
			return paths, err
		}
		path := strings.TrimSuffix(string(out), "\n")
		if !filepath.IsAbs(path) {
			path = filepath.Join(r.root, path)
		}
		relative, err := filepath.Rel(common, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return paths, &Error{Kind: KindUnsupported, Op: "prune", Message: "Git path escapes common directory"}
		}
		path = filepath.Join(canonical, relative)
		if err := safePrunePath(canonical, path, false); err != nil {
			return paths, err
		}
		if item.dst != nil {
			*item.dst = path
		} else {
			stat, err := os.Stat(path)
			if err != nil && !os.IsNotExist(err) {
				return paths, pruneIO("read ancestry override", err)
			}
			if err == nil && stat.Size() > 0 {
				return paths, &Error{Kind: KindUnsupported, Op: "prune", Message: "pruning shallow or grafted repositories is unsupported"}
			}
		}
	}
	for _, path := range []string{paths.ref + ".lock", paths.log + ".lock"} {
		if err := safePrunePath(canonical, path, false); err != nil {
			return paths, err
		}
	}
	return paths, nil
}

// Preserve existing storage permissions, including shared repositories. A packed
// ref's loose override inherits packed-refs' mode. New logs fall back to the ref.
func prunePublicationMode(paths prunePaths, target string) (os.FileMode, error) {
	candidates := []string{target}
	if target == paths.ref {
		candidates = append(candidates, filepath.Join(paths.common, "packed-refs"), paths.log)
	} else {
		candidates = append(candidates, paths.ref, filepath.Join(paths.common, "packed-refs"))
	}
	for _, path := range candidates {
		if err := safePrunePath(paths.common, path, false); err != nil {
			return 0, err
		}
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return 0, pruneIO("read publication permissions", err)
		}
		return info.Mode().Perm(), nil
	}
	parent := filepath.Dir(target)
	for {
		info, err := os.Stat(parent)
		if err == nil {
			return info.Mode().Perm() & 0666, nil
		}
		if !os.IsNotExist(err) {
			return 0, pruneIO("read parent permissions", err)
		}
		parent = filepath.Dir(parent)
	}
}

// safePrunePath rejects symlinks and non-directory ancestors below the canonical
// Git directory. This protects ordinary filesystem layouts, not hostile actors
// concurrently replacing Git directories outside Git's locking protocol.
func safePrunePath(common, path string, create bool) error {
	rel, err := filepath.Rel(common, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return &Error{Kind: KindInvalidRef, Op: "prune", Message: "unsafe Git path"}
	}
	current := common
	parts := strings.Split(rel, string(os.PathSeparator))
	for i, part := range parts {
		current = filepath.Join(current, part)
		st, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if create && i < len(parts)-1 {
				parent, parentErr := os.Stat(filepath.Dir(current))
				if parentErr != nil {
					return pruneIO("inspect parent Git directory", parentErr)
				}
				mode := parent.Mode() & (os.ModePerm | os.ModeSetgid)
				if err := os.Mkdir(current, mode); err != nil {
					if !os.IsExist(err) {
						return pruneIO("create Git directory", err)
					}
				} else if err := os.Chmod(current, mode); err != nil {
					return pruneIO("preserve Git directory permissions", err)
				}
				st, err = os.Lstat(current)
			} else {
				continue
			}
		}
		if err != nil {
			return pruneIO("inspect Git path", err)
		}
		if st.Mode()&os.ModeSymlink != 0 || i < len(parts)-1 && !st.IsDir() || i == len(parts)-1 && !st.Mode().IsRegular() {
			return &Error{Kind: KindInvalidRef, Op: "prune", Message: "unsafe symlink or nonregular Git path"}
		}
	}
	return nil
}

func pruneLock(common, path string, mode os.FileMode) (*os.File, error) {
	if err := safePrunePath(common, path, true); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if errors.Is(err, os.ErrExist) {
		return nil, &Error{Kind: KindConflict, Op: "prune", Message: "checkpoint publication lock already exists", Err: err}
	}
	if err != nil {
		return nil, pruneIO("lock checkpoint publication", err)
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, pruneIO("preserve publication permissions", err)
	}
	return f, nil
}
func stagePruneFile(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		return pruneIO("stage checkpoint publication", err)
	}
	if err := f.Sync(); err != nil {
		return pruneIO("sync checkpoint publication", err)
	}
	if err := f.Close(); err != nil {
		return pruneIO("close checkpoint publication", err)
	}
	return nil
}
func syncPruneDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return pruneIO("open publication directory", err)
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		return pruneIO("sync publication directory; inspect publication flags", err)
	}
	return nil
}

func (r *Repository) readPruneSnapshot(ctx context.Context, paths prunePaths, ref string, length int) (pruneSnapshot, error) {
	snapshot := pruneSnapshot{}
	for _, path := range []string{paths.ref, paths.log} {
		if err := safePrunePath(paths.common, path, false); err != nil {
			return snapshot, err
		}
	}
	if _, err := r.git(ctx, "symbolic-ref", "--quiet", ref); err == nil {
		return snapshot, invalidCheckpoint("symbolic checkpoint refs are unsupported")
	} else if !gitExitCode(err, 1) {
		return snapshot, err
	}
	refs, err := r.readRefSnapshot(ctx)
	if err != nil {
		return snapshot, err
	}
	snapshot.tip = refs[ref]
	raw, err := r.readPruneLog(paths.log)
	if err != nil && !os.IsNotExist(err) {
		if outputLimitError(err) {
			return snapshot, err
		}
		return snapshot, pruneIO("read checkpoint reflog", err)
	}
	snapshot.log = raw
	if len(raw) == 0 {
		return snapshot, nil
	}
	if raw[len(raw)-1] != '\n' {
		return snapshot, invalidCheckpoint("checkpoint reflog is not newline terminated")
	}
	for _, line := range strings.Split(string(raw[:len(raw)-1]), "\n") {
		old, rest, ok := strings.Cut(line, " ")
		if !ok {
			return snapshot, invalidCheckpoint("malformed checkpoint reflog")
		}
		next, suffix, ok := strings.Cut(rest, " ")
		if !ok || !validObjectID(old, length) || !validObjectID(next, length) || next == strings.Repeat("0", length) {
			return snapshot, invalidCheckpoint("malformed checkpoint reflog object IDs")
		}
		identity, _, _ := strings.Cut(suffix, "\t")
		fields := strings.Fields(identity)
		if len(fields) < 4 || !strings.Contains(identity, " <") || strings.ContainsRune(suffix, 0) {
			return snapshot, invalidCheckpoint("malformed checkpoint reflog identity")
		}
		if _, err := strconv.ParseInt(fields[len(fields)-2], 10, 64); err != nil {
			return snapshot, invalidCheckpoint("malformed checkpoint reflog timestamp")
		}
		zone := fields[len(fields)-1]
		if len(zone) != 5 || (zone[0] != '+' && zone[0] != '-') {
			return snapshot, invalidCheckpoint("malformed checkpoint reflog timezone")
		}
		if _, err := strconv.Atoi(zone[1:]); err != nil {
			return snapshot, invalidCheckpoint("malformed checkpoint reflog timezone")
		}
		snapshot.entries = append(snapshot.entries, pruneLogEntry{old, next, suffix})
	}
	if snapshot.tip == "" {
		return snapshot, invalidCheckpoint("checkpoint reflog exists without a live ref")
	}
	return snapshot, nil
}

func (r *Repository) readPruneLog(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var reader io.Reader = f
	if r.maxStdoutBytes > 0 && r.maxStdoutBytes < math.MaxInt64 {
		reader = io.LimitReader(f, r.maxStdoutBytes+1)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if r.maxStdoutBytes > 0 && int64(len(raw)) > r.maxStdoutBytes {
		return nil, &Error{Kind: KindOutputLimit, Op: "prune", Message: "checkpoint reflog exceeds stdout byte limit"}
	}
	return raw, nil
}

func (r *Repository) pruneInventory(ctx context.Context, snapshot pruneSnapshot, branch string, length int) (map[string]pruneCommit, []string, error) {
	commits := map[string]pruneCommit{}
	rank := []string{}
	direct := map[string]bool{}
	add := func(id string) {
		if !direct[id] {
			direct[id] = true
			rank = append(rank, id)
		}
	}
	add(snapshot.tip)
	for i := len(snapshot.entries) - 1; i >= 0; i-- {
		add(snapshot.entries[i].new)
	}
	queue := append([]string(nil), rank...)
	bases := map[string]bool{}
	trees := map[string]bool{}
	for len(queue) > 0 {
		batch := []string{}
		seen := map[string]bool{}
		for _, id := range queue {
			if _, ok := commits[id]; !ok && !seen[id] {
				seen[id] = true
				batch = append(batch, id)
			}
		}
		if len(batch) == 0 {
			break
		}
		objects, err := r.batchObjects(ctx, batch, true)
		if err != nil {
			return nil, nil, err
		}
		queue = nil
		for _, id := range batch {
			object := objects[id]
			if object.kind != "commit" {
				return nil, nil, invalidCheckpoint("checkpoint root or ancestor is unavailable")
			}
			cp, err := parseDiscoveryCheckpoint(id, object.raw, length)
			if err != nil {
				return nil, nil, err
			}
			if cp.Branch != branch {
				return nil, nil, invalidCheckpoint("checkpoint belongs to another branch")
			}
			headers, _, _ := bytes.Cut(object.raw, []byte("\n\n"))
			tree := ""
			for _, line := range strings.Split(string(headers), "\n") {
				if strings.HasPrefix(line, "tree ") {
					if tree != "" {
						return nil, nil, invalidCheckpoint("checkpoint has duplicate trees")
					}
					tree = strings.TrimPrefix(line, "tree ")
				}
			}
			if !validObjectID(tree, length) {
				return nil, nil, invalidCheckpoint("checkpoint tree is invalid")
			}
			trees[tree] = true
			bases[cp.Base] = true
			commits[id] = pruneCommit{cp, object.raw}
			if cp.Parent != cp.Base {
				queue = append(queue, cp.Parent)
			}
		}
	}
	baseIDs := sortedPruneKeys(bases)
	objects, err := r.batchObjects(ctx, baseIDs, true)
	if err != nil {
		return nil, nil, err
	}
	for _, id := range baseIDs {
		if objects[id].kind != "commit" {
			return nil, nil, invalidCheckpoint("checkpoint base commit is unavailable")
		}
		if checkpointCandidate(objects[id].raw) {
			return nil, nil, invalidCheckpoint("checkpoint base must be a normal commit")
		}
	}
	allIDs := append([]string(nil), baseIDs...)
	for id, item := range commits {
		allIDs = append(allIDs, id)
		cp := item.checkpoint
		if cp.Parent != cp.Base {
			parent := commits[cp.Parent].checkpoint
			if parent.Branch != cp.Branch || parent.Base != cp.Base {
				return nil, nil, invalidCheckpoint("checkpoint parent belongs to another stack")
			}
		}
	}
	sort.Strings(allIDs)
	if _, err := r.gitEnv(ctx, nil, objectBatchInput(allIDs), "rev-list", "--no-walk", "--stdin", "--"); err != nil {
		return nil, nil, err
	}
	// Walk each distinct snapshot tree once, independently of retained checkpoint
	// count. Verify tree/blob availability in one metadata batch before any write.
	dependencies := map[string]string{}
	for _, tree := range sortedPruneKeys(trees) {
		dependencies[tree] = "tree"
		out, err := r.git(ctx, "ls-tree", "-r", "-t", "-z", tree)
		if err != nil {
			return nil, nil, err
		}
		for _, entry := range bytes.Split(out, []byte{0}) {
			if len(entry) == 0 {
				continue
			}
			head, _, ok := bytes.Cut(entry, []byte{'\t'})
			fields := strings.Fields(string(head))
			if !ok || len(fields) != 3 || !validObjectID(fields[2], length) {
				return nil, nil, invalidCheckpoint("invalid checkpoint tree entry")
			}
			if fields[0] != "160000" {
				dependencies[fields[2]] = fields[1]
			}
		}
	}
	dependencyIDs := make([]string, 0, len(dependencies))
	for id := range dependencies {
		dependencyIDs = append(dependencyIDs, id)
	}
	sort.Strings(dependencyIDs)
	objects, err = r.batchObjects(ctx, dependencyIDs, false)
	if err != nil {
		return nil, nil, err
	}
	for _, id := range dependencyIDs {
		if objects[id].kind != dependencies[id] {
			return nil, nil, invalidCheckpoint("checkpoint tree or blob is unavailable or has invalid type")
		}
	}
	order := pruneParentOrder(commits)
	for i := len(order) - 1; i >= 0; i-- {
		if !direct[order[i]] {
			rank = append(rank, order[i])
		}
	}
	return commits, rank, nil
}

func sortedPruneKeys(values map[string]bool) []string {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Single-parent owned graphs permit a deterministic iterative topological walk.
func pruneParentOrder(commits map[string]pruneCommit) []string {
	ids := make([]string, 0, len(commits))
	for id := range commits {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	seen := map[string]bool{}
	order := []string{}
	for _, id := range ids {
		chain := []string{}
		for {
			if seen[id] {
				break
			}
			item, ok := commits[id]
			if !ok {
				break
			}
			seen[id] = true
			chain = append(chain, id)
			if item.checkpoint.Parent == item.checkpoint.Base {
				break
			}
			id = item.checkpoint.Parent
		}
		for i := len(chain) - 1; i >= 0; i-- {
			order = append(order, chain[i])
		}
	}
	return order
}

// Git hashes the canonical object header, NUL, and exact raw commit bytes.
func pruneObjectID(raw []byte, length int) string {
	var digest hash.Hash = sha1.New()
	if length == 64 {
		digest = sha256.New()
	}
	fmt.Fprintf(digest, "commit %d%c", len(raw), 0)
	_, _ = digest.Write(raw)
	return hex.EncodeToString(digest.Sum(nil))
}

// Selection rank is not publication chronology. Preserve retained original
// publications and their genuine revisits. Only synthesize uncovered maximal
// retained roots, at an original removed descendant's publication position.
func pruneReflog(snapshot pruneSnapshot, commits map[string]pruneCommit, keep map[string]bool, parents map[string]string, ids map[string]string, length int) ([]byte, error) {
	frontier := map[string]bool{}
	for id := range keep {
		frontier[id] = true
	}
	for id := range keep {
		delete(frontier, parents[id])
	}
	covered := map[string]bool{snapshot.tip: true}
	for _, entry := range snapshot.entries {
		if keep[entry.new] {
			covered[entry.new] = true
		}
	}
	positions := map[string]int{}
	for i, entry := range snapshot.entries {
		if keep[entry.new] {
			continue
		}
		id := entry.new
		for !keep[id] {
			cp := commits[id].checkpoint
			if cp.Parent == cp.Base {
				id = ""
				break
			}
			id = cp.Parent
		}
		if frontier[id] && !covered[id] {
			positions[id] = i
		}
	}
	synthetic := map[int]string{}
	for id := range frontier {
		if covered[id] {
			continue
		}
		position, ok := positions[id]
		if !ok {
			return nil, invalidCheckpoint("retained checkpoint frontier has no publication evidence")
		}
		synthetic[position] = id
	}
	type publication struct{ id, suffix string }
	rows := []publication{}
	emit := func(id, suffix string) {
		if len(rows) > 0 && rows[len(rows)-1].id == id {
			rows[len(rows)-1].suffix = suffix
			return
		}
		rows = append(rows, publication{id, suffix})
	}
	suffixes := map[string]string{}
	for i, entry := range snapshot.entries {
		suffixes[entry.new] = entry.suffix
		if keep[entry.new] {
			emit(entry.new, entry.suffix)
		} else if id := synthetic[i]; id != "" {
			identity, _, _ := strings.Cut(entry.suffix, "\t")
			emit(id, identity+"\tprune: retained ancestor")
		}
	}
	if len(rows) == 0 || rows[len(rows)-1].id != snapshot.tip {
		suffix := suffixes[snapshot.tip]
		if suffix == "" {
			headers, _, _ := bytes.Cut(commits[snapshot.tip].raw, []byte("\n\n"))
			for _, line := range strings.Split(string(headers), "\n") {
				if strings.HasPrefix(line, "committer ") {
					suffix = strings.TrimPrefix(line, "committer ") + "\tprune: retained current tip"
				}
			}
		}
		emit(snapshot.tip, suffix)
	}
	var log bytes.Buffer
	previous := strings.Repeat("0", length)
	for _, row := range rows {
		fmt.Fprintf(&log, "%s %s %s\n", previous, ids[row.id], row.suffix)
		previous = ids[row.id]
	}
	return log.Bytes(), nil
}
