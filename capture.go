package wip

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// Capture snapshots eligible working files over HEAD while preserving the real
// index and worktree. Publication uses the observed branch and checkpoint IDs.
func (r *Repository) Capture(ctx context.Context, options CaptureOptions) (CaptureResult, error) {
	if options.TrackedOnly && options.IncludeIgnored {
		return CaptureResult{}, &Error{Kind: KindInvalidOption, Op: "capture", Message: "tracked-only capture cannot include ignored files"}
	}
	if strings.ContainsRune(options.Message, 0) {
		return CaptureResult{}, &Error{Kind: KindInvalidOption, Op: "capture", Message: "checkpoint message must contain no NUL"}
	}
	info, err := r.Info(ctx)
	if err != nil {
		return CaptureResult{}, err
	}
	if info.Branch == "" || info.HEAD == "" {
		return CaptureResult{}, &Error{Kind: KindUnsupported, Op: "capture", Message: "capture requires an attached local branch with an existing commit"}
	}
	paths, err := r.normalizePaths(options.Paths)
	if err != nil {
		return CaptureResult{}, err
	}
	ref := checkpointRef(info.Branch)
	tip, err := r.validateTip(ctx, ref, info.Branch)
	if err != nil {
		return CaptureResult{}, err
	}
	parent := info.HEAD
	if tip.Commit != "" && tip.Base == info.HEAD {
		parent = tip.Commit
	}
	result := CaptureResult{Ref: ref, Parent: parent, HEAD: info.HEAD}
	eligible, err := r.capturePaths(ctx, info.HEAD, options, paths)
	if err != nil {
		return CaptureResult{}, err
	}
	temporary, err := os.MkdirTemp("", "go-git-wip-index-")
	if err != nil {
		return CaptureResult{}, &Error{Kind: KindGit, Op: "capture", Message: "create private index directory", Err: err}
	}
	defer os.RemoveAll(temporary)
	index := filepath.Join(temporary, "index")
	if _, err := r.indexGit(ctx, index, nil, "read-tree", info.HEAD); err != nil {
		return CaptureResult{}, err
	}
	if err := r.stageCapturePaths(ctx, index, eligible); err != nil {
		return CaptureResult{}, err
	}
	tree, err := r.indexGit(ctx, index, nil, "write-tree")
	if err != nil {
		return CaptureResult{}, err
	}
	parentTree, err := r.git(ctx, "rev-parse", "--verify", parent+"^{tree}")
	if err != nil {
		return CaptureResult{}, err
	}
	if bytes.Equal(bytes.TrimSpace(tree), bytes.TrimSpace(parentTree)) && !options.Baseline {
		if err := r.verifyCaptureHEAD(ctx, info); err != nil {
			return CaptureResult{}, err
		}
		return result, nil
	}
	message := options.Message
	if message == "" {
		message = "Checkpoint"
		if options.Baseline {
			message = "Baseline"
		}
	}
	metadata, err := json.Marshal(checkpointMetadata{Branch: info.Branch, Base: info.HEAD})
	if err != nil {
		return CaptureResult{}, &Error{Kind: KindInvalidOption, Op: "capture", Message: "encode checkpoint metadata", Err: err}
	}
	input := []byte(checkpointMarker + string(metadata) + "\n\n" + message + "\n")
	identity := checkpointIdentity()
	commit, err := r.gitEnv(ctx, identity, input, "commit-tree", "--no-gpg-sign", strings.TrimSpace(string(tree)), "-p", parent)
	if err != nil {
		return CaptureResult{}, err
	}
	if err := r.verifyCaptureHEAD(ctx, info); err != nil {
		return CaptureResult{}, err
	}
	oid := strings.TrimSpace(string(commit))
	if err := r.publishCapture(ctx, info, ref, oid, tip.Commit, identity); err != nil {
		return CaptureResult{}, err
	}
	if err := r.verifyCaptureHEAD(ctx, info); err != nil {
		return CaptureResult{}, err
	}
	result.Created, result.Commit = true, oid
	return result, nil
}

func (r *Repository) indexGit(ctx context.Context, index string, input []byte, args ...string) ([]byte, error) {
	controlled := []string{"-c", "core.splitIndex=false", "-c", "core.untrackedCache=false"}
	return r.gitEnv(ctx, []string{"GIT_INDEX_FILE=" + index}, input, append(controlled, args...)...)
}

func (r *Repository) capturePaths(ctx context.Context, head string, options CaptureOptions, selections []string) ([]string, error) {
	sparse, err := r.git(ctx, "config", "--bool", "--get", "core.sparseCheckout")
	if err != nil && !gitExitCode(err, 1) {
		return nil, err
	}
	if strings.TrimSpace(string(sparse)) == "true" {
		return nil, &Error{Kind: KindUnsupported, Op: "capture", Message: "sparse working trees are unsupported"}
	}
	index, err := r.git(ctx, "ls-files", "--stage", "-t", "-z")
	if err != nil {
		return nil, err
	}
	eligible := make(map[string]bool)
	for _, record := range bytes.Split(index, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		header, path, found := bytes.Cut(record, []byte{'\t'})
		fields := strings.Fields(string(header))
		if !found || len(fields) != 4 {
			return nil, &Error{Kind: KindGit, Op: "capture", Message: "invalid Git index entry"}
		}
		if fields[0] == "S" || fields[3] != "0" || fields[1] == "160000" {
			return nil, &Error{Kind: KindUnsupported, Op: "capture", Message: "sparse entries, unmerged indexes, and gitlinks are unsupported"}
		}
		eligible[string(path)] = true
	}
	tree, err := r.git(ctx, "ls-tree", "-r", "--full-tree", "-z", head)
	if err != nil {
		return nil, err
	}
	for _, record := range bytes.Split(tree, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		header, path, found := bytes.Cut(record, []byte{'\t'})
		fields := strings.Fields(string(header))
		if !found || len(fields) != 3 {
			return nil, &Error{Kind: KindGit, Op: "capture", Message: "invalid Git tree entry"}
		}
		if fields[0] == "160000" {
			return nil, &Error{Kind: KindUnsupported, Op: "capture", Message: "gitlinks are unsupported"}
		}
		eligible[string(path)] = true
	}
	if !options.TrackedOnly {
		args := []string{"ls-files", "--others", "--exclude-standard", "-z"}
		other, err := r.git(ctx, args...)
		if err != nil {
			return nil, err
		}
		addNULPaths(eligible, other)
		if options.IncludeIgnored {
			ignored, err := r.git(ctx, "ls-files", "--others", "--ignored", "--exclude-standard", "-z")
			if err != nil {
				return nil, err
			}
			addNULPaths(eligible, ignored)
		}
	}
	for _, selection := range selections {
		matched := false
		for path := range eligible {
			if pathSelected(path, []string{selection}) {
				matched = true
				break
			}
		}
		if !matched {
			return nil, &Error{Kind: KindInvalidOption, Op: "capture", Message: fmt.Sprintf("selection %q matches no eligible capture paths", selection)}
		}
	}
	result := make([]string, 0, len(eligible))
	for path := range eligible {
		if pathSelected(path, selections) {
			result = append(result, path)
		}
	}
	sort.Strings(result)
	return result, nil
}

func addNULPaths(paths map[string]bool, data []byte) {
	for _, path := range bytes.Split(data, []byte{0}) {
		if len(path) > 0 {
			paths[string(path)] = true
		}
	}
}

func (r *Repository) stageCapturePaths(ctx context.Context, index string, paths []string) error {
	var removed, added bytes.Buffer
	for _, path := range paths {
		if err := r.ensureNoSymlinkDirectories(path); err != nil {
			return err
		}
		info, err := os.Lstat(filepath.Join(r.root, filepath.FromSlash(path)))
		if os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR) || (err == nil && info.IsDir()) {
			if strings.HasSuffix(path, "/") {
				return &Error{Kind: KindUnsupported, Op: "capture", Message: "embedded repositories are unsupported"}
			}
			removed.WriteString(path)
			removed.WriteByte(0)
			continue
		}
		if err != nil {
			return &Error{Kind: KindGit, Op: "capture", Message: "inspect working file", Err: err}
		}
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return &Error{Kind: KindUnsupported, Op: "capture", Message: "only regular files and symlinks can be captured"}
		}
		added.WriteString(path)
		added.WriteByte(0)
	}
	if removed.Len() > 0 {
		if _, err := r.indexGit(ctx, index, removed.Bytes(), "update-index", "--force-remove", "-z", "--stdin"); err != nil {
			return err
		}
	}
	if added.Len() > 0 {
		if _, err := r.indexGit(ctx, index, added.Bytes(), "update-index", "--add", "-z", "--stdin"); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) verifyCaptureHEAD(ctx context.Context, observed RepositoryInfo) error {
	current, err := r.Info(ctx)
	if err != nil {
		return err
	}
	if current.Branch != observed.Branch || current.HEAD != observed.HEAD {
		return &Error{Kind: KindConflict, Op: "capture", Message: "work branch changed during capture"}
	}
	return nil
}

func (r *Repository) publishCapture(ctx context.Context, info RepositoryInfo, ref, commit, previous string, identity []string) error {
	if previous == "" {
		previous = strings.Repeat("0", len(info.HEAD))
	}
	input := []byte(fmt.Sprintf("start\nverify refs/heads/%s %s\nupdate %s %s %s\nprepare\ncommit\n", info.Branch, info.HEAD, ref, commit, previous))
	_, err := r.gitEnv(ctx, identity, input, "update-ref", "--no-deref", "--create-reflog", "-m", "go-git-wip checkpoint", "--stdin")
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		current, readErr := r.Info(ctx)
		if readErr != nil {
			return readErr
		}
		tip, readErr := r.validateTip(ctx, ref, info.Branch)
		if readErr != nil {
			if ctx.Err() != nil {
				return readErr
			}
			return &Error{Kind: KindConflict, Op: "capture", Message: "checkpoint ref changed during publication", Err: err}
		}
		observed := previous
		if observed == strings.Repeat("0", len(info.HEAD)) {
			observed = ""
		}
		if current.Branch != info.Branch || current.HEAD != info.HEAD || tip.Commit != observed {
			return &Error{Kind: KindConflict, Op: "capture", Message: "conditional checkpoint publication failed", Err: err}
		}
		return err
	}
	return nil
}
