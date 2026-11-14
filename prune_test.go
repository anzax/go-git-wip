package wip

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func pruneFixtureCommit(t *testing.T, f *testRepository, branch, base, parent, message string, when time.Time, legacy bool) string {
	t.Helper()
	body, err := encodeCheckpointMessage(branch, base, message)
	if err != nil {
		t.Fatal(err)
	}
	if legacy {
		body = []byte(fmt.Sprintf("go-git-wip:1 {\"branch\":%q,\"base\":%q}\n\n%s\n", branch, base, message))
	}
	raw := fmt.Sprintf("tree %s\nparent %s\nauthor Original Author <original@example.invalid> %d +0130\ncommitter Original Committer <committer@example.invalid> %d -0230\nencoding UTF-8\n\n%s", f.git("rev-parse", "HEAD^{tree}"), parent, when.Unix()-123, when.Unix(), body)
	return hashFixtureObject(t, f, "commit", raw)
}
func publishPruneFixture(f *testRepository, ref, id string) {
	f.git("update-ref", "--create-reflog", "-m", "original publication", ref, id)
}
func pruneStorage(t *testing.T, f *testRepository) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	err := filepath.WalkDir(filepath.Join(f.dir, ".git"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			result[path] = data
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestPruneRetentionAndExactStorage(t *testing.T) {
	for _, tc := range []struct {
		name             string
		last, days, want int
	}{{"count", 2, 0, 2}, {"days", 0, 3, 3}, {"union", 3, 3, 4}, {"newest-floor", 0, 1, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTestRepository(t, true)
			r := openFixture(t, f)
			ctx := context.Background()
			base := f.git("rev-parse", "HEAD")
			ref := "refs/wip/main"
			now := time.Now().UTC()
			ages := []int{2, 30, 1, 40, 50}
			parent := base
			original := map[string][]byte{}
			ids := []string{}
			for i, age := range ages {
				id := pruneFixtureCommit(t, f, "main", base, parent, fmt.Sprintf("same tree %d\n\n", i), now.Add(-time.Duration(age)*24*time.Hour), i%2 == 0)
				publishPruneFixture(f, ref, id)
				original[id] = []byte(f.git("cat-file", "commit", id) + "\n")
				ids = append(ids, id)
				parent = id
			}
			f.write("tracked.txt", "staged\n")
			f.git("add", "tracked.txt")
			f.write("tracked.txt", "working\n")
			index := f.read(".git/index")
			head := f.git("rev-parse", "HEAD")
			branch := f.git("rev-parse", "refs/heads/main")
			opts := PruneOptions{KeepLast: tc.last, KeepDays: tc.days, DryRun: true}
			storage := pruneStorage(t, f)
			result, err := r.Prune(ctx, opts)
			if err != nil || result.Before != 5 || result.Kept != tc.want || result.Pruned != 5-tc.want || result.Tip != parent {
				t.Fatalf("preview %+v %v", result, err)
			}
			if !reflect.DeepEqual(storage, pruneStorage(t, f)) {
				t.Fatal("dry run changed Git storage")
			}
			opts.DryRun = false
			result, err = r.Prune(ctx, opts)
			if err != nil || !result.RefPublished || !result.ReflogPublished {
				t.Fatalf("prune %+v %v", result, err)
			}
			if !bytes.Equal(index, f.read(".git/index")) || head != f.git("rev-parse", "HEAD") || branch != f.git("rev-parse", "refs/heads/main") || string(f.read("tracked.txt")) != "working\n" {
				t.Fatal("prune changed workspace")
			}
			history, err := r.History(ctx, HistoryOptions{})
			if err != nil || len(history) != tc.want {
				t.Fatalf("history %+v %v", history, err)
			}
			remaining := map[string]bool{}
			for _, cp := range history {
				remaining[cp.Commit] = true
				var oldID string
				for _, id := range ids {
					old, err := parseCheckpoint(id, original[id])
					if err != nil {
						t.Fatal(err)
					}
					if old.Message == cp.Message {
						oldID = id
						break
					}
				}
				if oldID == "" {
					t.Fatal("retained checkpoint lost original message")
				}
				raw := []byte(f.git("cat-file", "commit", cp.Commit) + "\n")
				old, _ := parseCheckpoint(oldID, original[oldID])
				expected := bytes.Replace(original[oldID], []byte("\nparent "+old.Parent+"\n"), []byte("\nparent "+cp.Parent+"\n"), 1)
				if !bytes.Equal(raw, expected) {
					t.Fatal("rewrite altered bytes beyond parent")
				}
				if _, err := r.Inspect(ctx, cp.Commit, CompareOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			log := f.read(".git/logs/refs/wip/main")
			for _, line := range strings.Split(strings.TrimSpace(string(log)), "\n") {
				fields := strings.Fields(line)
				for _, id := range fields[:2] {
					if id != strings.Repeat("0", 40) && !remaining[id] {
						t.Fatalf("discarded reflog root %s", id)
					}
				}
			}
			if entries, err := r.Reflog(ctx, ref, 0); err != nil || len(entries) != tc.want {
				t.Fatalf("reflog %+v %v", entries, err)
			}
			captureFixture(t, r, CaptureOptions{Baseline: true, Message: "after pruning"})
		})
	}
}

func TestPruneNoopEmptyAndPublicationRanking(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	ctx := context.Background()
	result, err := r.Prune(ctx, PruneOptions{KeepLast: 1})
	if err != nil || result.Before != 0 {
		t.Fatalf("empty %+v %v", result, err)
	}
	base := f.git("rev-parse", "HEAD")
	parent := base
	ids := []string{}
	for i := 0; i < 4; i++ {
		id := pruneFixtureCommit(t, f, "main", base, parent, strconv.Itoa(i), time.Now(), false)
		ids = append(ids, id)
		parent = id
	}
	// Only latest and oldest direct publications survive in the original log.
	publishPruneFixture(f, "refs/wip/main", ids[0])
	publishPruneFixture(f, "refs/wip/main", ids[3])
	// An existing publisher lock must not affect an exact no-op.
	f.write(".git/refs/wip/main.lock", "other writer\n")
	before := pruneStorage(t, f)
	result, err = r.Prune(ctx, PruneOptions{KeepLast: 100})
	if err != nil || result.Before != 4 || result.Pruned != 0 || !reflect.DeepEqual(before, pruneStorage(t, f)) {
		t.Fatalf("no-op %+v %v", result, err)
	}
	if err := os.Remove(filepath.Join(f.dir, ".git/refs/wip/main.lock")); err != nil {
		t.Fatal(err)
	}
	result, err = r.Prune(ctx, PruneOptions{KeepLast: 2})
	if err != nil || result.Kept != 2 {
		t.Fatalf("rank %+v %v", result, err)
	}
	history, err := r.History(ctx, HistoryOptions{})
	if err != nil || len(history) != 2 || history[0].Message != "3" || history[1].Message != "0" {
		t.Fatalf("publication ranking %+v %v", history, err)
	}
}

func TestPruneRolloversBranchesPackedRefsAndLinkedWorktree(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	ctx := context.Background()
	first := captureFixture(t, r, CaptureOptions{Baseline: true, Message: "first"})
	captureFixture(t, r, CaptureOptions{Baseline: true, Message: "second"})
	f.write("tracked.txt", "normal commit\n")
	f.git("add", ".")
	f.git("commit", "-m", "normal")
	third := captureFixture(t, r, CaptureOptions{Baseline: true, Message: "third"})
	captureFixture(t, r, CaptureOptions{Baseline: true, Message: "fourth"})
	f.git("switch", "-c", "other")
	other := captureFixture(t, r, CaptureOptions{Baseline: true, Message: "untouched"})
	f.git("switch", "main")
	f.git("pack-refs", "--all", "--prune")
	worktree := filepath.Join(t.TempDir(), "linked")
	f.git("worktree", "add", "--detach", worktree)
	linked, err := Open(ctx, worktree)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = linked.Prune(ctx, PruneOptions{KeepLast: 1}); err == nil {
		t.Fatal("detached default accepted")
	}
	// Explicit ref supports detached callers and changes only the selected branch.
	result, err := linked.Prune(ctx, PruneOptions{Ref: first.Ref, KeepLast: 3})
	if err != nil || result.Before != 4 || result.Kept != 3 {
		t.Fatalf("rollover %+v %v", result, err)
	}
	if f.git("rev-parse", other.Ref) != other.Commit {
		t.Fatal("other branch changed")
	}
	entries, err := r.Reflog(ctx, first.Ref, 0)
	if err != nil || len(entries) != 3 {
		t.Fatalf("entries %+v %v", entries, err)
	}
	messages := map[string]bool{}
	for _, entry := range entries {
		history, err := r.History(ctx, HistoryOptions{Revision: entry.Commit})
		if err != nil {
			t.Fatal(err)
		}
		for _, cp := range history {
			messages[cp.Message] = true
		}
	}
	if messages["first"] || !messages["second"] || !messages["third"] || !messages["fourth"] {
		t.Fatalf("retained stacks %+v; third %s", messages, third.Commit)
	}
}

func TestPruneRejectsUnsafeStatesBeforeWriting(t *testing.T) {
	for _, kind := range []string{"symbolic", "symlink-ref", "symlink-log", "symlink-lock", "shallow", "graft", "missing-root", "malformed-root", "missing-tree", "signed", "locked"} {
		t.Run(kind, func(t *testing.T) {
			f := newTestRepository(t, true)
			r := openFixture(t, f)
			ctx := context.Background()
			first := captureFixture(t, r, CaptureOptions{Baseline: true, Message: "first"})
			last := captureFixture(t, r, CaptureOptions{Baseline: true, Message: "last"})
			switch kind {
			case "symbolic":
				f.git("symbolic-ref", first.Ref, "refs/heads/main")
			case "symlink-ref", "symlink-log", "symlink-lock":
				path := filepath.Join(f.dir, ".git/refs/wip/main")
				if kind == "symlink-log" {
					path = filepath.Join(f.dir, ".git/logs/refs/wip/main")
				}
				if kind == "symlink-lock" {
					path += ".lock"
				} else {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(filepath.Join(f.dir, "tracked.txt"), path); err != nil {
					t.Fatal(err)
				}
			case "shallow":
				f.write(".git/shallow", first.HEAD+"\n")
			case "graft":
				f.write(".git/info/grafts", first.HEAD+"\n")
			case "missing-root":
				log := f.read(".git/logs/refs/wip/main")
				log = bytes.ReplaceAll(log, []byte(first.Commit), []byte(strings.Repeat("a", 40)))
				f.write(".git/logs/refs/wip/main", string(log))
			case "malformed-root":
				log := f.read(".git/logs/refs/wip/main")
				f.write(".git/logs/refs/wip/main", string(log)+"broken\n")
			case "missing-tree", "signed":
				raw := f.git("cat-file", "commit", last.Commit) + "\n"
				if kind == "missing-tree" {
					raw = strings.Replace(raw, "tree "+f.git("rev-parse", last.Commit+"^{tree}"), "tree "+strings.Repeat("b", 40), 1)
				} else {
					raw = strings.Replace(raw, "\n\n", "\ngpgsig fake-signature\n\n", 1)
				}
				id := hashFixtureObject(t, f, "commit", raw)
				publishPruneFixture(f, first.Ref, id)
			case "locked":
				f.write(".git/refs/wip/main.lock", "competing writer")
			}
			before := pruneStorage(t, f)
			_, err := r.Prune(ctx, PruneOptions{KeepLast: 1})
			if err == nil {
				t.Fatal("unsafe state succeeded")
			}
			if !reflect.DeepEqual(before, pruneStorage(t, f)) {
				t.Fatalf("unsafe state mutated Git storage: %v", err)
			}
		})
	}
}

func TestPruneInvalidOptionsAndCancellation(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	for _, options := range []PruneOptions{{}, {KeepLast: -1}, {KeepDays: -1}, {KeepDays: int(^uint(0) >> 1)}} {
		_, err := r.Prune(context.Background(), options)
		var detail *Error
		if !errors.As(err, &detail) || detail.Kind != KindInvalidOption {
			t.Fatalf("options %+v: %v", options, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.Prune(ctx, PruneOptions{KeepLast: 1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestPrunePreservesDivergentAncestry(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	base := f.git("rev-parse", "HEAD")
	now := time.Now()
	old := now.Add(-30 * 24 * time.Hour)
	a := pruneFixtureCommit(t, f, "main", base, base, "a", now, false)
	b := pruneFixtureCommit(t, f, "main", base, a, "b", old, false)
	d := pruneFixtureCommit(t, f, "main", base, a, "d", old, false)
	e := pruneFixtureCommit(t, f, "main", base, b, "e", old, false)
	for _, id := range []string{a, b, d, e} {
		publishPruneFixture(f, "refs/wip/main", id)
	}
	result, err := r.Prune(context.Background(), PruneOptions{KeepLast: 2, KeepDays: 2})
	if err != nil || result.Kept != 3 {
		t.Fatalf("fork %+v %v", result, err)
	}
	entries, err := r.Reflog(context.Background(), result.Ref, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		cp, err := r.resolveCheckpoint(context.Background(), entry.Commit)
		if err != nil {
			t.Fatal(err)
		}
		if (cp.Message == "d" || cp.Message == "e") && cp.Parent != a {
			t.Fatalf("fork flattened %+v", cp)
		}
	}
}

func TestPruneSHA256AndUnsupportedBackend(t *testing.T) {
	for _, format := range []string{"sha256", "reftable"} {
		t.Run(format, func(t *testing.T) {
			f := newTestRepository(t, false)
			if err := os.RemoveAll(filepath.Join(f.dir, ".git")); err != nil {
				t.Fatal(err)
			}
			flag := "--object-format=sha256"
			if format == "reftable" {
				flag = "--ref-format=reftable"
			}
			cmd := exec.Command(f.gitPath, "init", "--initial-branch=main", "--template=", flag)
			cmd.Dir = f.dir
			cmd.Env = f.env
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Skipf("Git lacks %s support: %v %s", format, err, out)
			}
			f.write("tracked.txt", "initial\n")
			f.git("add", ".")
			f.git("commit", "-m", "initial")
			r := openFixture(t, f)
			captureFixture(t, r, CaptureOptions{Baseline: true, Message: "first"})
			last := captureFixture(t, r, CaptureOptions{Baseline: true, Message: "last"})
			before := pruneStorage(t, f)
			result, err := r.Prune(context.Background(), PruneOptions{KeepLast: 1})
			if format == "reftable" {
				var detail *Error
				if !errors.As(err, &detail) || detail.Kind != KindUnsupported || !reflect.DeepEqual(before, pruneStorage(t, f)) {
					t.Fatalf("backend %+v %v", result, err)
				}
				return
			}
			if err != nil || len(result.Tip) != 64 || result.Tip == last.Commit {
				t.Fatalf("SHA256 %+v %v", result, err)
			}
			history, err := r.History(context.Background(), HistoryOptions{})
			if err != nil || len(history) != 1 || history[0].Parent != last.HEAD {
				t.Fatalf("SHA256 history %+v %v", history, err)
			}
		})
	}
}

func TestPruneCompetingSnapshotAndPartialPublication(t *testing.T) {
	for _, mode := range []string{"tip", "same-tip-log", "partial-ref-failure", "mid-cancel"} {
		t.Run(mode, func(t *testing.T) {
			f := newTestRepository(t, true)
			r := openFixture(t, f)
			captureFixture(t, r, CaptureOptions{Baseline: true, Message: "first"})
			tip := captureFixture(t, r, CaptureOptions{Baseline: true, Message: "last"})
			marker := filepath.Join(t.TempDir(), "once")
			// The wrapper simulates precisely controlled races at the operation boundary;
			// all mutations remain inside this disposable fixture.
			script := fmt.Sprintf("if test -f .git/refs/wip/main.lock && ! test -f %q; then\n", marker)
			switch mode {
			case "tip":
				script += fmt.Sprintf("case \"$*\" in *show-ref*) touch %q; printf '%%s\\n' %q > .git/refs/wip/main;; esac\n", marker, tip.HEAD)
			case "same-tip-log":
				script += fmt.Sprintf("case \"$*\" in *show-ref*) touch %q; tail -n 1 .git/logs/refs/wip/main >> .git/logs/refs/wip/main;; esac\n", marker)
			case "partial-ref-failure":
				script += fmt.Sprintf("case \"$*\" in *hash-object*) touch %q; rm .git/refs/wip/main; mkdir .git/refs/wip/main;; esac\n", marker)
			case "mid-cancel":
				script += fmt.Sprintf("case \"$*\" in *hash-object*) touch %q; sleep 10;; esac\n", marker)
			}
			script += "fi"
			refReadWrapper(t, f, r, script)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "mid-cancel" {
				go func() {
					for i := 0; i < 1000; i++ {
						if _, err := os.Stat(marker); err == nil {
							cancel()
							return
						}
						time.Sleep(time.Millisecond)
					}
				}()
			}
			result, err := r.Prune(ctx, PruneOptions{KeepLast: 1})
			if err == nil {
				t.Fatalf("race succeeded %+v", result)
			}
			switch mode {
			case "tip", "same-tip-log":
				var detail *Error
				if !errors.As(err, &detail) || detail.Kind != KindConflict || result.ReflogPublished || result.RefPublished {
					t.Fatalf("conflict %+v %v", result, err)
				}
			case "partial-ref-failure":
				if !result.ReflogPublished || result.RefPublished || result.Tip != tip.Commit {
					t.Fatalf("partial flags %+v %v", result, err)
				}
				log := f.read(".git/logs/refs/wip/main")
				if strings.Contains(string(log), tip.Commit) {
					t.Fatal("partial new log retained superseded tip")
				}
			case "mid-cancel":
				if !errors.Is(err, context.Canceled) || result.ReflogPublished || result.RefPublished {
					t.Fatalf("cancel %+v %v", result, err)
				}
			}
			for _, path := range []string{".git/refs/wip/main.lock", ".git/logs/refs/wip/main.lock"} {
				if _, err := os.Lstat(filepath.Join(f.dir, path)); !os.IsNotExist(err) {
					t.Fatalf("lock left after failure: %s %v", path, err)
				}
			}
		})
	}
}

func TestPruneReflogByteLimit(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	captureFixture(t, r, CaptureOptions{Baseline: true})
	captureFixture(t, r, CaptureOptions{Baseline: true})
	// Exercise the direct-file reader's bound without depending on unrelated Git
	// command output sizes; both snapshot passes share this reader.
	r.maxStdoutBytes = 4
	_, err := r.readPruneLog(filepath.Join(f.dir, ".git/logs/refs/wip/main"))
	var detail *Error
	if !errors.As(err, &detail) || detail.Kind != KindOutputLimit {
		t.Fatalf("raw log limit: %v", err)
	}
}

func TestPruneNativeEmptyMessageReflog(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	base := f.git("rev-parse", "HEAD")
	now := time.Now()
	first := pruneFixtureCommit(t, f, "main", base, base, "first", now, false)
	last := pruneFixtureCommit(t, f, "main", base, first, "last", now, false)
	// Native update-ref without -m omits the tab/message separator entirely.
	f.git("update-ref", "--create-reflog", "refs/wip/main", first)
	f.git("update-ref", "--create-reflog", "refs/wip/main", last)
	original := f.read(".git/logs/refs/wip/main")
	if bytes.Contains(original, []byte{'\t'}) {
		t.Fatal("fixture did not exercise native message-less log")
	}
	result, err := r.Prune(context.Background(), PruneOptions{KeepLast: 1})
	if err != nil || result.Pruned != 1 {
		t.Fatalf("empty message %+v %v", result, err)
	}
	newLog := f.read(".git/logs/refs/wip/main")
	if bytes.Contains(newLog, []byte{'\t'}) {
		t.Fatal("native empty-message suffix changed")
	}
	if entries, err := r.Reflog(context.Background(), result.Ref, 0); err != nil || len(entries) != 1 || entries[0].Message != "" {
		t.Fatalf("empty reflog %+v %v", entries, err)
	}
}

func TestPruneRejectsCollapsingRetainedCheckpoints(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	base := f.git("rev-parse", "HEAD")
	now := time.Now()
	p := pruneFixtureCommit(t, f, "main", base, base, "discarded parent", now, false)
	a := pruneFixtureCommit(t, f, "main", base, base, "identical", now, false)
	b := pruneFixtureCommit(t, f, "main", base, p, "identical", now, false)
	for _, id := range []string{p, a, b} {
		publishPruneFixture(f, "refs/wip/main", id)
	}
	before := pruneStorage(t, f)
	for _, dry := range []bool{true, false} {
		result, err := r.Prune(context.Background(), PruneOptions{KeepLast: 2, DryRun: dry})
		var detail *Error
		if !errors.As(err, &detail) || detail.Kind != KindUnsupported || result.RefPublished || result.ReflogPublished {
			t.Fatalf("collapse %+v %v", result, err)
		}
		if !reflect.DeepEqual(before, pruneStorage(t, f)) {
			t.Fatal("rejected collision mutated storage")
		}
	}
}

func TestPrunePreservesSharedPermissions(t *testing.T) {
	previous := syscall.Umask(0022)
	t.Cleanup(func() { syscall.Umask(previous) })
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprintf("packed=%t", packed), func(t *testing.T) {
			f := newTestRepository(t, true)
			f.git("init", "--shared=group")
			r := openFixture(t, f)
			captureFixture(t, r, CaptureOptions{Baseline: true, Message: "first"})
			captureFixture(t, r, CaptureOptions{Baseline: true, Message: "last"})
			refPath := filepath.Join(f.dir, ".git/refs/wip/main")
			logPath := filepath.Join(f.dir, ".git/logs/refs/wip/main")
			if packed {
				f.git("pack-refs", "--all", "--prune")
				if err := os.Remove(filepath.Dir(refPath)); err != nil {
					t.Fatal(err)
				}
			}
			result, err := r.Prune(context.Background(), PruneOptions{KeepLast: 1})
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{refPath, logPath} {
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0664 {
					t.Fatalf("mode %s %v %v", path, info, err)
				}
			}
			info, err := os.Stat(filepath.Dir(refPath))
			if err != nil || info.Mode().Perm()&0030 != 0030 || info.Mode()&os.ModeSetgid == 0 {
				t.Fatalf("shared ref directory %v %v", info, err)
			}
			if !result.RefPublished {
				t.Fatal("not published")
			}
		})
	}
}

func TestPruneReflogChronologyAndFrontierCoverage(t *testing.T) {
	for _, scenario := range []string{"ancestry-only-middle", "no-synthetic-revisit", "unlogged-frontier", "genuine-revisit"} {
		t.Run(scenario, func(t *testing.T) {
			f := newTestRepository(t, true)
			r := openFixture(t, f)
			base := f.git("rev-parse", "HEAD")
			now := time.Now()
			old := now.Add(-30 * 24 * time.Hour)
			create := func(parent, message string, when time.Time) string {
				return pruneFixtureCommit(t, f, "main", base, parent, message, when, false)
			}
			expected := []string{}
			options := PruneOptions{KeepDays: 1}
			switch scenario {
			case "ancestry-only-middle":
				a := create(base, "a", old)
				b := create(a, "b", now)
				c := create(b, "c", old)
				d := create(c, "d", old)
				publishPruneFixture(f, "refs/wip/main", a)
				publishPruneFixture(f, "refs/wip/main", d)
				options.KeepLast = 2
				expected = []string{"a", "d"}
			default:
				b := create(base, "b", now)
				a := create(b, "a", now)
				p := create(a, "p", old)
				q := create(b, "q", old)
				switch scenario {
				case "no-synthetic-revisit":
					for _, id := range []string{b, a, p, q} {
						publishPruneFixture(f, "refs/wip/main", id)
					}
					expected = []string{"b", "a", "r"}
				case "unlogged-frontier":
					publishPruneFixture(f, "refs/wip/main", p)
					expected = []string{"a", "r"}
				case "genuine-revisit":
					for _, id := range []string{b, a, b, p} {
						publishPruneFixture(f, "refs/wip/main", id)
					}
					expected = []string{"b", "a", "b", "r"}
				}
				f.write("tracked.txt", "new base\n")
				f.git("add", ".")
				f.git("commit", "-m", "new base")
				base = f.git("rev-parse", "HEAD")
				live := create(base, "r", old)
				publishPruneFixture(f, "refs/wip/main", live)
			}
			result, err := r.Prune(context.Background(), options)
			if err != nil {
				t.Fatal(err)
			}
			entries, err := r.Reflog(context.Background(), result.Ref, 0)
			if err != nil {
				t.Fatal(err)
			}
			messages := []string{}
			reachable := map[string]bool{}
			for i := len(entries) - 1; i >= 0; i-- {
				cp, err := r.resolveCheckpoint(context.Background(), entries[i].Commit)
				if err != nil {
					t.Fatal(err)
				}
				messages = append(messages, cp.Message)
				history, err := r.History(context.Background(), HistoryOptions{Revision: cp.Commit})
				if err != nil {
					t.Fatal(err)
				}
				for _, cp := range history {
					reachable[cp.Commit] = true
				}
			}
			if !reflect.DeepEqual(messages, expected) || len(reachable) != result.Kept {
				t.Fatalf("chronology %v expected %v; reachable %d kept %d", messages, expected, len(reachable), result.Kept)
			}
			beforeRepeat := pruneStorage(t, f)
			repeated, err := r.Prune(context.Background(), options)
			if err != nil || repeated.Pruned != 0 || repeated.ReflogPublished || repeated.RefPublished || !reflect.DeepEqual(beforeRepeat, pruneStorage(t, f)) {
				t.Fatalf("repeat retention mutated storage %+v %v", repeated, err)
			}
		})
	}
}
