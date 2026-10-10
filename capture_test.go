package wip

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openFixture(t *testing.T, f *testRepository) *Repository {
	t.Helper()
	r, err := Open(context.Background(), f.dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func captureFixture(t *testing.T, r *Repository, options CaptureOptions) CaptureResult {
	t.Helper()
	result, err := r.Capture(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCaptureBaselineUnchangedAndFixedIdentity(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	f.git("config", "--unset", "user.name")
	f.git("config", "--unset", "user.email")
	t.Setenv("GIT_AUTHOR_NAME", "Inherited")
	t.Setenv("GIT_COMMITTER_EMAIL", "wrong@example.invalid")
	t.Setenv("GIT_AUTHOR_DATE", "2000-01-01T00:00:00Z")
	t.Setenv("GIT_COMMITTER_DATE", "invalid inherited date")
	unchanged := captureFixture(t, r, CaptureOptions{})
	if unchanged.Created || unchanged.Commit != "" || unchanged.Parent != unchanged.HEAD || unchanged.Ref != "refs/wip/main" {
		t.Fatalf("unexpected unchanged capture: %+v", unchanged)
	}
	start := time.Now().Add(-time.Second)
	baseline := captureFixture(t, r, CaptureOptions{Baseline: true})
	if !baseline.Created || baseline.Parent != baseline.HEAD || f.git("rev-parse", baseline.Ref) != baseline.Commit {
		t.Fatalf("unexpected baseline: %+v", baseline)
	}
	identity := f.git("show", "-s", "--format=%an|%ae|%cn|%ce", baseline.Commit)
	if identity != "WIP checkpoint|wip@localhost|WIP checkpoint|wip@localhost" {
		t.Fatalf("wrong checkpoint identity: %s", identity)
	}
	cp, err := r.resolveCheckpoint(context.Background(), baseline.Commit)
	if err != nil || cp.Base != baseline.HEAD || cp.Branch != "main" || cp.Message != "Baseline" || cp.Time.Before(start) || cp.Time.After(time.Now().Add(time.Second)) {
		t.Fatalf("unexpected metadata/time: %+v, %v", cp, err)
	}
	unchanged = captureFixture(t, r, CaptureOptions{})
	if unchanged.Created || unchanged.Parent != baseline.Commit || f.git("rev-parse", baseline.Ref) != baseline.Commit {
		t.Fatalf("unchanged advanced ref: %+v", unchanged)
	}
	forced := captureFixture(t, r, CaptureOptions{Baseline: true, Message: "deliberate start"})
	if !forced.Created || forced.Parent != baseline.Commit {
		t.Fatalf("forced baseline did not create child: %+v", forced)
	}
	message := "multiline\n\ntrailing newline\n"
	multiline := captureFixture(t, r, CaptureOptions{Baseline: true, Message: message})
	cp, err = r.resolveCheckpoint(context.Background(), multiline.Commit)
	if err != nil || cp.Message != message {
		t.Fatalf("message did not roundtrip: %q, %v", cp.Message, err)
	}
}

func TestCapturePreservesPartialStagingAndTrackedEligibility(t *testing.T) {
	f := newTestRepository(t, true)
	f.write("deleted.txt", "delete me\n")
	f.write("staged deletion.txt", "keep working copy\n")
	f.git("add", ".")
	f.git("commit", "-m", "tracked files")
	f.write("tracked.txt", "staged\n")
	f.git("add", "tracked.txt")
	f.write("tracked.txt", "working\n")
	f.write("added.txt", "staged addition\n")
	f.git("add", "added.txt")
	f.write("added.txt", "working addition\n")
	f.git("rm", "--cached", "staged deletion.txt")
	if err := os.Remove(filepath.Join(f.dir, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	index, working := f.read(".git/index"), f.read("tracked.txt")
	head := f.git("rev-parse", "HEAD")
	r := openFixture(t, f)
	result := captureFixture(t, r, CaptureOptions{})
	if !result.Created || result.HEAD != head || result.Parent != head {
		t.Fatalf("unexpected capture: %+v", result)
	}
	for path, contents := range map[string]string{"tracked.txt": "working", "added.txt": "working addition", "staged deletion.txt": "keep working copy"} {
		if got := f.git("show", result.Commit+":"+path); got != contents {
			t.Fatalf("%s captured %q, want %q", path, got, contents)
		}
	}
	if got := f.git("ls-tree", "--name-only", result.Commit); strings.Contains(got, "deleted.txt") {
		t.Fatalf("deleted path remained: %s", got)
	}
	if !bytes.Equal(index, f.read(".git/index")) || !bytes.Equal(working, f.read("tracked.txt")) || f.git("rev-parse", "HEAD") != head || f.git("show", ":tracked.txt") != "staged" {
		t.Fatal("capture changed index, HEAD, staged contents, or working file")
	}
	if captureFixture(t, r, CaptureOptions{}).Created {
		t.Fatal("same working tree created another checkpoint")
	}
}

func TestCaptureUntrackedIgnoredAndLiteralPaths(t *testing.T) {
	f := newTestRepository(t, true)
	f.write(".gitignore", "ignored*\n")
	f.git("add", ".gitignore")
	f.git("commit", "-m", "ignore policy")
	for path, data := range map[string]string{
		"untracked space.txt": "new", "ignored secret.txt": "ignored",
		"literal[1].txt": "bracket", "literal1.txt": "other", "line\nbreak\t.txt": "odd bytes",
	} {
		f.write(path, data)
	}
	r := openFixture(t, f)
	selected := captureFixture(t, r, CaptureOptions{Paths: []string{"untracked space.txt", "ignored secret.txt"}})
	if selected.Created {
		t.Fatal("explicit paths bypassed untracked policy")
	}
	newFiles := captureFixture(t, r, CaptureOptions{IncludeUntracked: true})
	if got := f.git("show", newFiles.Commit+":line\nbreak\t.txt"); got != "odd bytes" {
		t.Fatalf("odd filename not captured: %q", got)
	}
	if strings.Contains(f.git("ls-tree", "--name-only", newFiles.Commit), "ignored secret") {
		t.Fatal("ignored file included by untracked option")
	}
	ignored := captureFixture(t, r, CaptureOptions{IncludeUntracked: true, IncludeIgnored: true})
	if f.git("show", ignored.Commit+":ignored secret.txt") != "ignored" {
		t.Fatal("ignored option did not include file")
	}
	literal := captureFixture(t, r, CaptureOptions{IncludeUntracked: true, Paths: []string{"literal[1].txt"}})
	if got := f.git("ls-tree", "--name-only", literal.Commit); !strings.Contains(got, "literal[1].txt") || strings.Contains(got, "literal1.txt") || strings.Contains(got, "untracked space") {
		t.Fatalf("literal selection expanded or retained unselected edits: %q", got)
	}
	_, err := r.Capture(context.Background(), CaptureOptions{IncludeIgnored: true})
	requireErrorKind(t, err, KindInvalidOption)
}

func TestPartialCaptureUsesFreshHEADAndSubdirectoryPaths(t *testing.T) {
	f := newTestRepository(t, true)
	f.write("nested/a space.txt", "original a\n")
	f.write("nested/b.txt", "original b\n")
	f.git("add", ".")
	f.git("commit", "-m", "two files")
	f.write("nested/a space.txt", "changed a\n")
	f.write("nested/b.txt", "changed b\n")
	r, err := Open(context.Background(), filepath.Join(f.dir, "nested"))
	if err != nil {
		t.Fatal(err)
	}
	a := captureFixture(t, r, CaptureOptions{Paths: []string{"a space.txt"}})
	b := captureFixture(t, r, CaptureOptions{Paths: []string{"b.txt"}})
	if b.Parent != a.Commit || f.git("show", b.Commit+":nested/a space.txt") != "original a" || f.git("show", b.Commit+":nested/b.txt") != "changed b" {
		t.Fatalf("partial capture was cumulative: %+v", b)
	}
	if err := os.Remove(filepath.Join(f.dir, "nested", "b.txt")); err != nil {
		t.Fatal(err)
	}
	directory := captureFixture(t, r, CaptureOptions{Paths: []string{"."}})
	if got := f.git("ls-tree", "-r", "--name-only", directory.Commit); strings.Contains(got, "nested/b.txt") {
		t.Fatalf("directory selection missed tracked deletion: %q", got)
	}
}

func TestCaptureRolloverAndBranchIsolation(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	baseline := captureFixture(t, r, CaptureOptions{Baseline: true})
	f.write("tracked.txt", "checkpoint one\n")
	one := captureFixture(t, r, CaptureOptions{})
	f.git("add", "tracked.txt")
	f.git("commit", "-m", "normal advancement")
	f.write("tracked.txt", "checkpoint two\n")
	two := captureFixture(t, r, CaptureOptions{})
	if two.Parent != two.HEAD || two.Parent == one.Commit || one.Parent != baseline.Commit {
		t.Fatalf("wrong rollover parents: %+v, %+v", one, two)
	}
	if old := f.git("rev-parse", "refs/wip/main@{1}"); old != one.Commit {
		t.Fatalf("rollover discarded retained tip: %s", old)
	}
	f.git("switch", "-c", "feature/branch")
	other := captureFixture(t, r, CaptureOptions{})
	if other.Ref != "refs/wip/feature/branch" || other.Parent != other.HEAD || f.git("rev-parse", "refs/wip/main") != two.Commit {
		t.Fatalf("branch checkpoints interfered: %+v", other)
	}
}

func TestCaptureRejectsUnsupportedStates(t *testing.T) {
	t.Run("unborn", func(t *testing.T) {
		f := newTestRepository(t, false)
		_, err := openFixture(t, f).Capture(context.Background(), CaptureOptions{})
		requireErrorKind(t, err, KindUnsupported)
	})
	t.Run("detached", func(t *testing.T) {
		f := newTestRepository(t, true)
		f.git("switch", "--detach")
		_, err := openFixture(t, f).Capture(context.Background(), CaptureOptions{})
		requireErrorKind(t, err, KindUnsupported)
	})
	t.Run("sparse", func(t *testing.T) {
		f := newTestRepository(t, true)
		f.git("update-index", "--skip-worktree", "tracked.txt")
		_, err := openFixture(t, f).Capture(context.Background(), CaptureOptions{})
		requireErrorKind(t, err, KindUnsupported)
	})
	t.Run("gitlink", func(t *testing.T) {
		f := newTestRepository(t, true)
		f.git("update-index", "--add", "--cacheinfo", "160000,"+f.git("rev-parse", "HEAD")+",module")
		_, err := openFixture(t, f).Capture(context.Background(), CaptureOptions{})
		requireErrorKind(t, err, KindUnsupported)
	})
	t.Run("unmerged", func(t *testing.T) {
		f := newTestRepository(t, true)
		f.git("switch", "-c", "other")
		f.write("tracked.txt", "other\n")
		f.git("add", ".")
		f.git("commit", "-m", "other")
		f.git("switch", "main")
		f.write("tracked.txt", "main\n")
		f.git("add", ".")
		f.git("commit", "-m", "main")
		cmd := exec.Command(f.gitPath, "merge", "other")
		cmd.Dir, cmd.Env = f.dir, f.env
		if err := cmd.Run(); err == nil {
			t.Fatal("fixture merge should conflict")
		}
		index := f.read(".git/index")
		_, err := openFixture(t, f).Capture(context.Background(), CaptureOptions{})
		requireErrorKind(t, err, KindUnsupported)
		if !bytes.Equal(index, f.read(".git/index")) {
			t.Fatal("unmerged index was modified")
		}
	})
}

func TestCaptureRejectsUnownedCheckpointRefs(t *testing.T) {
	variants := []string{
		"ordinary commit", "go-git-wip:2 {}\n\nfuture",
		`go-git-wip:1 {"branch":"other","base":"%s"}` + "\n\nwrong branch",
		`go-git-wip:1 {"branch":"main","base":"short"}` + "\n\nbad base",
		`go-git-wip:1 {"branch":"main","branch":"main","base":"%s"}` + "\n\nduplicate",
		`go-git-wip:1 {"branch":"main","base":"%s","extra":true}` + "\n\nextra",
	}
	for _, variant := range variants {
		t.Run(variant, func(t *testing.T) {
			f := newTestRepository(t, true)
			head := f.git("rev-parse", "HEAD")
			message := variant
			if strings.Contains(message, "%s") {
				message = fmt.Sprintf(message, head)
			}
			tip := f.git("commit-tree", f.git("rev-parse", "HEAD^{tree}"), "-p", head, "-m", message)
			f.git("update-ref", "refs/wip/main", tip)
			_, err := openFixture(t, f).Capture(context.Background(), CaptureOptions{Baseline: true})
			requireErrorKind(t, err, KindInvalidRef)
			if f.git("rev-parse", "refs/wip/main") != tip {
				t.Fatal("unowned checkpoint ref was overwritten")
			}
		})
	}
	t.Run("symbolic", func(t *testing.T) {
		f := newTestRepository(t, true)
		f.git("symbolic-ref", "refs/wip/main", "refs/heads/main")
		_, err := openFixture(t, f).Capture(context.Background(), CaptureOptions{Baseline: true})
		requireErrorKind(t, err, KindInvalidRef)
		if f.git("symbolic-ref", "refs/wip/main") != "refs/heads/main" {
			t.Fatal("symbolic ref was overwritten")
		}
	})
	t.Run("malformed loose ref", func(t *testing.T) {
		f := newTestRepository(t, true)
		f.write(".git/refs/wip/main", "broken\n")
		_, err := openFixture(t, f).Capture(context.Background(), CaptureOptions{Baseline: true})
		requireErrorKind(t, err, KindInvalidRef)
		if string(f.read(".git/refs/wip/main")) != "broken\n" {
			t.Fatal("malformed ref was overwritten")
		}
	})
}

// A repository-configured clean filter gives a real Git barrier after the capture
// observed its parent and before publication. No production injection seam is needed.
func blockingCaptureFilter(t *testing.T, f *testRepository) (ready, release string) {
	t.Helper()
	dir := t.TempDir()
	ready, release = filepath.Join(dir, "ready"), filepath.Join(dir, "release")
	program := filepath.Join(dir, "clean")
	script := "#!/bin/sh\nprintf ready > \"$1\"\nwhile test ! -e \"$2\"; do sleep 0.01; done\ncat\n"
	if err := os.WriteFile(program, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	f.git("config", "filter.wip-test.clean", quote(program)+" "+quote(ready)+" "+quote(release))
	f.git("config", "filter.wip-test.required", "true")
	f.write(".gitattributes", "tracked.txt filter=wip-test\n")
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0600) })
	return ready, release
}

func waitForCaptureFilter(t *testing.T, ready string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(ready); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("capture filter did not reach barrier")
}

func TestCaptureCompetingPublicationAndBranchChange(t *testing.T) {
	for _, branchChange := range []bool{false, true} {
		t.Run(fmt.Sprintf("branchChange=%v", branchChange), func(t *testing.T) {
			f := newTestRepository(t, true)
			r := openFixture(t, f)
			baseline := captureFixture(t, r, CaptureOptions{Baseline: true})
			ready, release := blockingCaptureFilter(t, f)
			f.write("tracked.txt", "new content\n")
			index := f.read(".git/index")
			done := make(chan error, 1)
			go func() { _, err := r.Capture(context.Background(), CaptureOptions{}); done <- err }()
			waitForCaptureFilter(t, ready)
			winner := baseline.Commit
			if branchChange {
				f.git("commit", "--allow-empty", "-m", "concurrent normal commit")
			} else {
				winner = f.git("commit-tree", f.git("rev-parse", "HEAD^{tree}"), "-p", baseline.Commit, "-m", fmt.Sprintf("go-git-wip:1 {\"branch\":\"main\",\"base\":\"%s\"}\n\nwinner", baseline.HEAD))
				f.git("update-ref", "refs/wip/main", winner, baseline.Commit)
			}
			if err := os.WriteFile(release, nil, 0600); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				requireErrorKind(t, err, KindConflict)
			case <-time.After(5 * time.Second):
				t.Fatal("capture did not finish")
			}
			if f.git("rev-parse", "refs/wip/main") != winner || !bytes.Equal(index, f.read(".git/index")) {
				t.Fatal("competing capture overwrote winner or changed index")
			}
		})
	}
}

func TestCaptureCancellationCleansPrivateIndex(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	ready, release := blockingCaptureFilter(t, f)
	f.write("tracked.txt", "changed\n")
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	index := f.read(".git/index")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := r.Capture(ctx, CaptureOptions{}); done <- err }()
	waitForCaptureFilter(t, ready)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled capture did not finish")
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(temporary)
	if err != nil || len(entries) != 0 || !bytes.Equal(index, f.read(".git/index")) {
		t.Fatalf("cancelled capture left private files or changed index: %v, %v", entries, err)
	}
}

func TestCaptureFileDirectoryReplacementsAndSelection(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	if err := os.Remove(filepath.Join(f.dir, "tracked.txt")); err != nil {
		t.Fatal(err)
	}
	f.write("tracked.txt/child.txt", "new directory content\n")
	index := f.read(".git/index")
	_, err := r.Capture(context.Background(), CaptureOptions{IncludeUntracked: true, Paths: []string{"tracked.txt/child.txt"}})
	if err == nil || !bytes.Equal(index, f.read(".git/index")) {
		t.Fatal("partial replacement discarded an unselected HEAD file or changed index")
	}
	whole := captureFixture(t, r, CaptureOptions{IncludeUntracked: true})
	if f.git("show", whole.Commit+":tracked.txt/child.txt") != "new directory content" {
		t.Fatal("file to directory replacement not captured")
	}
	f.git("add", ".")
	f.git("commit", "-m", "directory in HEAD")
	if err := os.RemoveAll(filepath.Join(f.dir, "tracked.txt")); err != nil {
		t.Fatal(err)
	}
	f.write("tracked.txt", "regular file again\n")
	file := captureFixture(t, r, CaptureOptions{IncludeUntracked: true})
	if f.git("show", file.Commit+":tracked.txt") != "regular file again" {
		t.Fatal("directory to file replacement not captured")
	}
}

func TestCaptureSHA256AndPrivateIndexCleanup(t *testing.T) {
	f := newTestRepository(t, false)
	// Reinitialize a fresh fixture with the alternate native object format.
	if err := os.RemoveAll(filepath.Join(f.dir, ".git")); err != nil {
		t.Fatal(err)
	}
	f.git("init", "--object-format=sha256", "--initial-branch=main", "--template=")
	f.write("tracked.txt", "sha256\n")
	f.git("add", ".")
	f.git("commit", "-m", "SHA-256 base")
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	baseline := captureFixture(t, openFixture(t, f), CaptureOptions{Baseline: true})
	if len(baseline.Commit) != 64 || len(baseline.HEAD) != 64 {
		t.Fatalf("SHA-256 IDs were truncated: %+v", baseline)
	}
	entries, err := os.ReadDir(temporary)
	if err != nil || len(entries) != 0 {
		t.Fatalf("successful capture left private index: %v, %v", entries, err)
	}
}

func TestCaptureRejectsIndirectTagAndInconsistentParent(t *testing.T) {
	t.Run("tag target", func(t *testing.T) {
		f := newTestRepository(t, true)
		r := openFixture(t, f)
		baseline := captureFixture(t, r, CaptureOptions{Baseline: true})
		f.git("tag", "-a", "checkpoint-tag", baseline.Commit, "-m", "tag checkpoint")
		tag := f.git("rev-parse", "refs/tags/checkpoint-tag")
		f.git("update-ref", "refs/wip/main", tag)
		_, err := r.Capture(context.Background(), CaptureOptions{})
		requireErrorKind(t, err, KindInvalidRef)
	})
	t.Run("unowned parent", func(t *testing.T) {
		f := newTestRepository(t, true)
		head := f.git("rev-parse", "HEAD")
		tree := f.git("rev-parse", "HEAD^{tree}")
		unowned := f.git("commit-tree", tree, "-p", head, "-m", "ordinary parent")
		tip := f.git("commit-tree", tree, "-p", unowned, "-m", fmt.Sprintf("go-git-wip:1 {\"branch\":\"main\",\"base\":\"%s\"}\n\nchild", head))
		f.git("update-ref", "refs/wip/main", tip)
		_, err := openFixture(t, f).Capture(context.Background(), CaptureOptions{})
		requireErrorKind(t, err, KindInvalidRef)
	})
}
