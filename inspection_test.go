package wip

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func openInspectionRepository(t *testing.T, f *testRepository, directory string) *Repository {
	t.Helper()
	if directory == "" {
		directory = f.dir
	}
	r, err := Open(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func captureInspection(t *testing.T, r *Repository, options CaptureOptions) CaptureResult {
	t.Helper()
	result, err := r.Capture(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created {
		t.Fatal("expected checkpoint creation")
	}
	return result
}

func requireInspectionKind(t *testing.T, err error, kind ErrorKind) {
	t.Helper()
	var detail *Error
	if !errors.As(err, &detail) || detail.Kind != kind {
		t.Fatalf("error = %v; want kind %s", err, kind)
	}
}

func TestInspectionHistoryRolloverAndReflog(t *testing.T) {
	ctx := context.Background()
	f := newTestRepository(t, true)
	r := openInspectionRepository(t, f, "")
	status, err := r.Status(ctx)
	if err != nil || status.Active || status.Tip != "" || status.Ref != "refs/wip/main" {
		t.Fatalf("initial status = %+v, %v", status, err)
	}
	history, err := r.History(ctx, HistoryOptions{})
	if err != nil || len(history) != 0 {
		t.Fatalf("empty history = %+v, %v", history, err)
	}
	base := captureInspection(t, r, CaptureOptions{Baseline: true, Message: "review start"})
	f.write("tracked.txt", "changed\n")
	first := captureInspection(t, r, CaptureOptions{Message: "first edit"})
	f.write("other file.txt", "other\n")
	second := captureInspection(t, r, CaptureOptions{Message: "second edit"})
	history, err = r.History(ctx, HistoryOptions{})
	if err != nil || len(history) != 3 {
		t.Fatalf("history = %+v, %v", history, err)
	}
	if history[0].Commit != second.Commit || history[1].Commit != first.Commit || history[2].Commit != base.Commit || history[2].Parent != base.HEAD {
		t.Fatalf("unexpected parent stack: %+v", history)
	}
	if history[2].Message != "review start" || history[2].Time.IsZero() {
		t.Fatalf("public checkpoint metadata = %+v", history[2])
	}
	filtered, err := r.History(ctx, HistoryOptions{Paths: []string{"tracked.txt"}, Limit: 1})
	if err != nil || len(filtered) != 1 || filtered[0].Commit != first.Commit {
		t.Fatalf("matching limit = %+v, %v", filtered, err)
	}
	f.git("add", "--", "tracked.txt")
	f.git("commit", "-m", "normal advancement")
	status, err = r.Status(ctx)
	if err != nil || status.Active || status.Tip != second.Commit || status.Base != base.HEAD {
		t.Fatalf("stale status = %+v, %v", status, err)
	}
	rolled := captureInspection(t, r, CaptureOptions{Baseline: true, Message: "new stack"})
	if rolled.Parent != f.git("rev-parse", "HEAD") {
		t.Fatalf("rollover parent = %s, HEAD = %s", rolled.Parent, rolled.HEAD)
	}
	history, err = r.History(ctx, HistoryOptions{})
	if err != nil || len(history) != 1 || history[0].Commit != rolled.Commit {
		t.Fatalf("rolled history = %+v, %v", history, err)
	}
	log, err := r.Reflog(ctx, rolled.Ref, 0)
	if err != nil || len(log) != 4 || log[1].Commit != second.Commit || log[1].Selector != rolled.Ref+"@{1}" {
		t.Fatalf("retained log = %+v, %v", log, err)
	}
	old, err := r.History(ctx, HistoryOptions{Revision: log[1].Selector})
	if err != nil || len(old) != 3 || old[0].Commit != second.Commit {
		t.Fatalf("reflog history = %+v, %v", old, err)
	}
	// Repeated tips are distinct retained records rather than silently deduplicated.
	f.git("update-ref", "-m", "revisit old stack", rolled.Ref, second.Commit)
	f.git("update-ref", "-m", "return to current stack", rolled.Ref, rolled.Commit)
	log, err = r.Reflog(ctx, rolled.Ref, 3)
	if err != nil || len(log) != 3 || log[0].Commit != log[2].Commit || log[0].Selector == log[2].Selector {
		t.Fatalf("duplicate reflog records = %+v, %v", log, err)
	}
	status, err = r.Status(ctx)
	if err != nil || !status.Active {
		t.Fatalf("active status = %+v, %v", status, err)
	}
	_, err = r.History(ctx, HistoryOptions{Limit: -1})
	requireInspectionKind(t, err, KindInvalidOption)
	_, err = r.Reflog(ctx, rolled.Ref, -1)
	requireInspectionKind(t, err, KindInvalidOption)
	_, err = r.History(ctx, HistoryOptions{Revision: "HEAD"})
	requireInspectionKind(t, err, KindInvalidRef)
}

func TestInspectionComparisonAndExactFileBytes(t *testing.T) {
	ctx := context.Background()
	f := newTestRepository(t, true)
	weird := "dir/a\tb\nfile.txt"
	for path, content := range map[string]string{
		weird: "before\n", "binary.dat": "\x00one\xff", "deleted.txt": "gone\n", "type.txt": "regular\n", "rename-old.txt": "rename\n",
	} {
		f.write(path, content)
	}
	f.git("add", "--", ".")
	f.git("commit", "-m", "comparison fixture")
	r := openInspectionRepository(t, f, "")
	base := captureInspection(t, r, CaptureOptions{Baseline: true})
	f.write(weird, "after\nextra\n")
	f.write("binary.dat", "\x00two\xfe")
	f.write("tracked.txt", "initial\nmore\n")
	f.write(":(glob)*.txt", "literal\n")
	if err := os.Remove(filepath.Join(f.dir, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.dir, "type.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("tracked.txt", filepath.Join(f.dir, "type.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(f.dir, "rename-old.txt"), filepath.Join(f.dir, "rename-new.txt")); err != nil {
		t.Fatal(err)
	}
	current := captureInspection(t, r, CaptureOptions{})
	// These commands fail if Git invokes a configured external diff or textconv.
	f.write(".git/info/attributes", "binary.dat diff=unsafe\n")
	f.git("config", "diff.external", "false")
	f.git("config", "diff.unsafe.textconv", "false")
	f.git("config", "diff.renames", "true")
	comparison, err := r.Inspect(ctx, current.Commit, CompareOptions{IncludePatch: true})
	if err != nil || comparison.From != base.Commit || comparison.To != current.Commit || comparison.Patch == "" {
		t.Fatalf("inspection = %+v, %v", comparison, err)
	}
	changes := make(map[string]FileChange)
	for _, file := range comparison.Files {
		changes[file.Path] = file
	}
	for path, want := range map[string]FileChange{
		weird:            {Path: weird, Status: "M", Added: 2, Deleted: 1},
		"binary.dat":     {Path: "binary.dat", Status: "M", Binary: true},
		"deleted.txt":    {Path: "deleted.txt", Status: "D", Deleted: 1},
		"tracked.txt":    {Path: "tracked.txt", Status: "M", Added: 1},
		"type.txt":       {Path: "type.txt", Status: "T", Added: 1, Deleted: 1},
		"rename-old.txt": {Path: "rename-old.txt", Status: "D", Deleted: 1},
		"rename-new.txt": {Path: "rename-new.txt", Status: "A", Added: 1},
		":(glob)*.txt":   {Path: ":(glob)*.txt", Status: "A", Added: 1},
	} {
		if changes[path] != want {
			t.Errorf("change %q = %+v; want %+v", path, changes[path], want)
		}
	}
	if len(changes) != 8 {
		t.Fatalf("unexpected changes: %+v", comparison.Files)
	}
	literal, err := r.Compare(ctx, "HEAD", current.Ref, CompareOptions{Paths: []string{":(glob)*.txt"}})
	if err != nil || len(literal.Files) != 1 || literal.Files[0].Path != ":(glob)*.txt" || literal.Patch != "" {
		t.Fatalf("literal path comparison = %+v, %v", literal, err)
	}
	sub := openInspectionRepository(t, f, filepath.Join(f.dir, "dir"))
	filtered, err := sub.Compare(ctx, base.Commit, current.Commit, CompareOptions{Paths: []string{"a\tb\nfile.txt"}})
	if err != nil || len(filtered.Files) != 1 || filtered.Files[0].Path != weird {
		t.Fatalf("subdirectory comparison = %+v, %v", filtered, err)
	}
	for path, want := range map[string][]byte{"binary.dat": []byte("\x00two\xfe"), "type.txt": []byte("tracked.txt"), weird: []byte("after\nextra\n")} {
		got, err := r.ReadFile(ctx, current.Commit, path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("ReadFile(%q) = %q, %v; want %q", path, got, err, want)
		}
	}
	_, err = r.ReadFile(ctx, current.Commit, "deleted.txt")
	requireInspectionKind(t, err, KindNotFound)
	_, err = r.ReadFile(ctx, current.Commit, "dir")
	requireInspectionKind(t, err, KindInvalidOption)
	_, err = r.Compare(ctx, "--output=bad", current.Commit, CompareOptions{})
	requireInspectionKind(t, err, KindNotFound)
	_, err = r.Compare(ctx, base.Commit, current.Commit, CompareOptions{Paths: []string{"../outside"}})
	requireInspectionKind(t, err, KindInvalidOption)
}

func TestInspectionRefDeletionAndPartialCleanup(t *testing.T) {
	ctx := context.Background()
	f := newTestRepository(t, true)
	r := openInspectionRepository(t, f, "")
	main := captureInspection(t, r, CaptureOptions{Baseline: true})
	f.git("switch", "-c", "feature/topic")
	topic := captureInspection(t, r, CaptureOptions{Baseline: true})
	f.git("switch", "-c", "abandoned")
	abandoned := captureInspection(t, r, CaptureOptions{Baseline: true})
	f.git("switch", "main")
	f.git("branch", "-D", "feature/topic", "abandoned")
	refs, err := r.Refs(ctx)
	if err != nil || len(refs) != 3 || refs[0].Ref != abandoned.Ref || refs[1].Ref != topic.Ref || refs[2].Ref != main.Ref || !refs[0].Orphaned || !refs[1].Orphaned || refs[2].Orphaned {
		t.Fatalf("owned refs = %+v, %v", refs, err)
	}
	beforeHead, beforeBranch := f.git("rev-parse", "HEAD"), f.git("rev-parse", "refs/heads/main")
	f.write("tracked.txt", "staged\n")
	f.git("add", "--", "tracked.txt")
	f.write("tracked.txt", "working\n")
	beforeIndex, beforeWork := f.read(".git/index"), f.read("tracked.txt")
	_, err = r.Delete(ctx, DeleteOptions{Ref: main.Ref, ExpectedTip: topic.Commit})
	requireInspectionKind(t, err, KindConflict)
	cleanup, err := r.Cleanup(ctx, []DeleteOptions{
		{Ref: abandoned.Ref, ExpectedTip: abandoned.Commit},
		{Ref: main.Ref, ExpectedTip: main.Commit},
		{Ref: topic.Ref, ExpectedTip: main.Commit},
		{Ref: topic.Ref, ExpectedTip: topic.Commit},
	})
	if err != nil || !reflect.DeepEqual(cleanup.Deleted, []string{abandoned.Ref, topic.Ref}) || len(cleanup.Failures) != 2 {
		t.Fatalf("partial cleanup = %+v, %v", cleanup, err)
	}
	requireInspectionKind(t, cleanup.Failures[0].Err, KindInvalidOption)
	requireInspectionKind(t, cleanup.Failures[1].Err, KindConflict)
	missing, err := r.Delete(ctx, DeleteOptions{Ref: topic.Ref, ExpectedTip: topic.Commit})
	if err != nil || missing.Deleted {
		t.Fatalf("missing deletion = %+v, %v", missing, err)
	}
	deleted, err := r.Delete(ctx, DeleteOptions{Ref: main.Ref, ExpectedTip: main.Commit})
	if err != nil || !deleted.Deleted {
		t.Fatalf("deletion = %+v, %v", deleted, err)
	}
	if f.git("rev-parse", "HEAD") != beforeHead || f.git("rev-parse", "refs/heads/main") != beforeBranch || !bytes.Equal(f.read(".git/index"), beforeIndex) || !bytes.Equal(f.read("tracked.txt"), beforeWork) {
		t.Fatal("ref management changed HEAD, normal branch, index bytes, or working content")
	}
	log, err := r.Reflog(ctx, main.Ref, 0)
	if err != nil || len(log) != 0 {
		t.Fatalf("deleted ref reflog = %+v, %v", log, err)
	}
	_, err = r.Delete(ctx, DeleteOptions{Ref: main.Ref, ExpectedTip: main.Commit[:8]})
	requireInspectionKind(t, err, KindInvalidOption)
	_, err = r.Delete(ctx, DeleteOptions{Ref: "refs/heads/main", ExpectedTip: beforeHead})
	requireInspectionKind(t, err, KindInvalidRef)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = r.Cleanup(cancelled, []DeleteOptions{{Ref: main.Ref, ExpectedTip: main.Commit}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cleanup cancellation = %v", err)
	}
}

func TestInspectionDetachedUnbornAndInvalidRefs(t *testing.T) {
	ctx := context.Background()
	f := newTestRepository(t, false)
	r := openInspectionRepository(t, f, "")
	status, err := r.Status(ctx)
	if err != nil || status.Active || status.Repository.HEAD != "" {
		t.Fatalf("unborn status = %+v, %v", status, err)
	}
	f.write("tracked.txt", "initial\n")
	f.git("add", "--", ".")
	f.git("commit", "-m", "initial")
	baseline := captureInspection(t, r, CaptureOptions{Baseline: true})
	f.git("switch", "--detach")
	status, err = r.Status(ctx)
	if err != nil || status.Active || status.Ref != "" || status.Repository.Branch != "" {
		t.Fatalf("detached status = %+v, %v", status, err)
	}
	history, err := r.History(ctx, HistoryOptions{Revision: baseline.Commit})
	if err != nil || len(history) != 1 {
		t.Fatalf("detached explicit history = %+v, %v", history, err)
	}
	f.git("update-ref", "refs/wip/unowned", "HEAD")
	_, err = r.Refs(ctx)
	requireInspectionKind(t, err, KindInvalidRef)
	_, err = r.Delete(ctx, DeleteOptions{Ref: "refs/wip/unowned", ExpectedTip: baseline.HEAD})
	requireInspectionKind(t, err, KindInvalidRef)
	f.git("update-ref", "-d", "refs/wip/unowned")
	f.git("symbolic-ref", "refs/wip/alias", baseline.Ref)
	_, err = r.Refs(ctx)
	requireInspectionKind(t, err, KindInvalidRef)
	f.git("symbolic-ref", "--delete", "refs/wip/alias")
	f.write(".git/refs/wip/broken", strings.Repeat("a", len(baseline.Commit))+"\n")
	_, err = r.Refs(ctx)
	requireInspectionKind(t, err, KindInvalidRef)
}
