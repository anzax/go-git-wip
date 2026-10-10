package wip

import (
	"bytes"
	"context"
	"strconv"
	"strings"
)

// Inspect compares an owned checkpoint to its single parent.
func (r *Repository) Inspect(ctx context.Context, revision string, options CompareOptions) (Comparison, error) {
	cp, err := r.resolveCheckpoint(ctx, revision)
	if err != nil {
		return Comparison{}, err
	}
	if cp.Parent != cp.Base {
		parent, err := r.resolveCheckpoint(ctx, cp.Parent)
		if err != nil {
			return Comparison{}, err
		}
		if parent.Branch != cp.Branch || parent.Base != cp.Base {
			return Comparison{}, &Error{Kind: KindInvalidRef, Op: "inspect", Message: "checkpoint parent belongs to another stack"}
		}
	}
	paths, err := r.normalizePaths(options.Paths)
	if err != nil {
		return Comparison{}, err
	}
	return r.compareCommits(ctx, cp.Parent, cp.Commit, paths, options.IncludePatch)
}

// Compare resolves two explicit commit revisions and returns structured changes.
// Rename detection, external diff programs, and text conversion are disabled.
func (r *Repository) Compare(ctx context.Context, from, to string, options CompareOptions) (Comparison, error) {
	fromID, err := r.resolveCommit(ctx, from)
	if err != nil {
		return Comparison{}, err
	}
	toID, err := r.resolveCommit(ctx, to)
	if err != nil {
		return Comparison{}, err
	}
	paths, err := r.normalizePaths(options.Paths)
	if err != nil {
		return Comparison{}, err
	}
	return r.compareCommits(ctx, fromID, toID, paths, options.IncludePatch)
}

func (r *Repository) compareCommits(ctx context.Context, from, to string, paths []string, patch bool) (Comparison, error) {
	result := Comparison{From: from, To: to, Files: []FileChange{}}
	args := []string{"diff", "--no-renames", "--no-ext-diff", "--no-textconv", "--no-color"}
	run := func(format ...string) ([]byte, error) {
		call := append([]string{}, args...)
		call = append(call, format...)
		call = append(call, from, to, "--")
		call = append(call, paths...)
		return r.git(ctx, call...)
	}
	status, err := run("--name-status", "-z")
	if err != nil {
		return Comparison{}, err
	}
	fields := bytes.Split(status, []byte{0})
	byPath := make(map[string]int)
	for i := 0; i+1 < len(fields); i += 2 {
		letter, path := string(fields[i]), string(fields[i+1])
		if len(letter) != 1 || !strings.Contains("AMDT", letter) || path == "" {
			return Comparison{}, &Error{Kind: KindGit, Op: "compare", Message: "unexpected Git changed-file record"}
		}
		byPath[path] = len(result.Files)
		result.Files = append(result.Files, FileChange{Path: path, Status: letter})
	}
	numstat, err := run("--numstat", "-z")
	if err != nil {
		return Comparison{}, err
	}
	for _, record := range bytes.Split(numstat, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		counts := strings.SplitN(string(record), "\t", 3)
		if len(counts) != 3 {
			return Comparison{}, &Error{Kind: KindGit, Op: "compare", Message: "unexpected Git line-count record"}
		}
		index, found := byPath[counts[2]]
		if !found {
			return Comparison{}, &Error{Kind: KindGit, Op: "compare", Message: "Git line-count path has no changed-file record"}
		}
		file := &result.Files[index]
		if counts[0] == "-" && counts[1] == "-" {
			file.Binary = true
			continue
		}
		file.Added, err = strconv.Atoi(counts[0])
		if err == nil {
			file.Deleted, err = strconv.Atoi(counts[1])
		}
		if err != nil || file.Added < 0 || file.Deleted < 0 {
			return Comparison{}, &Error{Kind: KindGit, Op: "compare", Message: "invalid Git line counts", Err: err}
		}
	}
	if patch {
		out, err := run("--patch")
		if err != nil {
			return Comparison{}, err
		}
		result.Patch = string(out)
	}
	return result, nil
}

// ReadFile returns exact blob bytes at a commit revision, including binary files
// and stored symlink targets. Path is relative to the invocation directory.
func (r *Repository) ReadFile(ctx context.Context, revision, path string) ([]byte, error) {
	commit, err := r.resolveCommit(ctx, revision)
	if err != nil {
		return nil, err
	}
	paths, err := r.normalizePaths([]string{path})
	if err != nil {
		return nil, err
	}
	out, err := r.git(ctx, "ls-tree", "--full-tree", "-z", commit, "--", paths[0])
	if err != nil {
		return nil, err
	}
	for _, record := range bytes.Split(out, []byte{0}) {
		metadata, name, found := bytes.Cut(record, []byte{'\t'})
		if !found || string(name) != paths[0] {
			continue
		}
		fields := strings.Fields(string(metadata))
		if len(fields) == 3 && fields[1] == "blob" {
			return r.git(ctx, "cat-file", "blob", fields[2])
		}
		return nil, &Error{Kind: KindInvalidOption, Op: "read-file", Message: "selected path is not a file"}
	}
	return nil, &Error{Kind: KindNotFound, Op: "read-file", Message: "file does not exist in selected commit"}
}
