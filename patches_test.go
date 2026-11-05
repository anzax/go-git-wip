package wip

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComparePatchesMatchesCompareAndResolvesOnce(t *testing.T) {
	f := newTestRepository(t, true)
	paths := []string{"tracked.txt", "quoted\tline\nfile.txt", ":(glob)*.txt"}
	for _, path := range paths {
		f.write(path, "old source\n")
	}
	f.git("add", ".")
	f.git("commit", "-m", "before")
	base := f.git("rev-parse", "HEAD")
	for _, path := range paths {
		f.write(path, "new source\n")
	}
	r := openFixture(t, f)
	saved := captureFixture(t, r, CaptureOptions{})
	want := map[string]string{}
	for _, path := range paths {
		compared, err := r.Compare(context.Background(), base, saved.Commit, CompareOptions{Paths: []string{path}, IncludePatch: true})
		if err != nil {
			t.Fatal(err)
		}
		want[path] = compared.Patch
	}
	log := filepath.Join(t.TempDir(), "commands")
	wrapper := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$PATCH_TEST_LOG\"\nexec \"$PATCH_TEST_GIT\" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATCH_TEST_LOG", log)
	t.Setenv("PATCH_TEST_GIT", r.gitPath)
	r.gitPath = wrapper
	got, err := r.ComparePatches(context.Background(), base, saved.Commit, append(paths, paths[0]))
	if err != nil {
		t.Fatal(err)
	}
	if got.From != base || got.To != saved.Commit || len(got.Files) != len(paths) {
		t.Fatalf("unexpected result: %+v", got)
	}
	for i, file := range got.Files {
		if file.Path != paths[i] || file.Patch != want[file.Path] {
			t.Fatalf("patch differs for %q", file.Path)
		}
	}
	commands, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(commands), " rev-parse ") != 2 || strings.Count(string(commands), " diff ") != len(paths) || strings.Contains(string(commands), "--numstat") || strings.Contains(string(commands), "--name-status") {
		t.Fatalf("redundant commands: %s", commands)
	}
}

func TestComparePatchesRejectsUnsafeInputsAndBoundsAggregate(t *testing.T) {
	f := newTestRepository(t, true)
	for _, path := range []string{"first.txt", "second.txt"} {
		f.write(path, strings.Repeat("changed source\n", 20))
	}
	r := openFixture(t, f)
	saved := captureFixture(t, r, CaptureOptions{})
	paths := []string{"first.txt", "second.txt"}
	got, err := r.ComparePatches(context.Background(), saved.HEAD, saved.Commit, paths)
	if err != nil {
		t.Fatal(err)
	}
	limit := 0
	for _, file := range got.Files {
		if len(file.Patch) > limit {
			limit = len(file.Patch)
		}
	}
	r.maxStdoutBytes = int64(limit + 1)
	_, err = r.ComparePatches(context.Background(), saved.HEAD, saved.Commit, paths)
	var detail *Error
	if !errors.As(err, &detail) || detail.Kind != KindOutputLimit {
		t.Fatalf("aggregate limit: %v", err)
	}
	r.maxStdoutBytes = 0
	for _, paths := range [][]string{nil, {""}, {"../outside"}, {"first.txt\x00bad"}} {
		if _, err := r.ComparePatches(context.Background(), saved.HEAD, saved.Commit, paths); err == nil {
			t.Fatalf("accepted invalid paths %q", paths)
		}
	}
	if _, err := r.ComparePatches(context.Background(), "--output=outside", saved.Commit, paths); err == nil {
		t.Fatal("accepted option-like revision")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.ComparePatches(ctx, saved.HEAD, saved.Commit, paths); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestComparePatchesRootProofAndSymlinkDirectory(t *testing.T) {
	f := newTestRepository(t, false)
	f.write("dir/document.txt", "root source\n")
	f.git("add", ".")
	f.git("commit", "-m", "root source")
	head := f.git("rev-parse", "HEAD")
	r := openFixture(t, f)
	got, err := r.ComparePatches(context.Background(), "empty", head, []string{"dir/document.txt"})
	if err != nil || got.From != "empty" || len(got.Files) != 1 || !strings.Contains(got.Files[0].Patch, "+root source") {
		t.Fatalf("root patch: %+v %v", got, err)
	}
	f.write("dir/document.txt", "later source\n")
	f.git("add", ".")
	f.git("commit", "-m", "later source")
	if _, err = r.ComparePatches(context.Background(), "empty", "HEAD", []string{"dir/document.txt"}); err == nil {
		t.Fatal("accepted false empty baseline")
	}
	shallowTip := f.git("rev-parse", "HEAD")
	f.write(".git/shallow", shallowTip+"\n")
	if _, err = r.ComparePatches(context.Background(), "empty", shallowTip, []string{"dir/document.txt"}); err == nil {
		t.Fatal("accepted shallow boundary as a root commit")
	}
	if err = os.Rename(filepath.Join(f.dir, "dir"), filepath.Join(f.dir, "original-dir")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(t.TempDir(), filepath.Join(f.dir, "dir")); err != nil {
		t.Fatal(err)
	}
	for _, from := range []string{"empty", head} {
		if _, err = r.ComparePatches(context.Background(), from, head, []string{"dir/document.txt"}); err == nil {
			t.Fatalf("accepted live symlink ancestor from %s", from)
		}
	}
}
