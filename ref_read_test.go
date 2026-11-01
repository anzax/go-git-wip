package wip

import (
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

func refReadWrapper(t *testing.T, f *testRepository, r *Repository, before string) string {
	t.Helper()
	dir := t.TempDir()
	log, wrapper := filepath.Join(dir, "commands"), filepath.Join(dir, "git")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + quote(log) + "\n" + before + "\nexec " + quote(f.gitPath) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	r.gitPath = wrapper
	return log
}

func hashFixtureObject(t *testing.T, f *testRepository, kind, raw string) string {
	t.Helper()
	cmd := exec.Command(f.gitPath, "hash-object", "-w", "-t", kind, "--stdin")
	cmd.Dir, cmd.Env, cmd.Stdin = f.dir, f.env, strings.NewReader(raw)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestRefReadsBatchValidationAndFreshness(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	head := f.git("rev-parse", "HEAD")
	for i := 23; i >= 0; i-- {
		branch := fmt.Sprintf("topic/%02d", i)
		parent := commitWithRawMessage(t, f, head, checkpointBody(t, branch, head, "parent"))
		tip := commitWithRawMessage(t, f, parent, checkpointBody(t, branch, head, "tip"))
		f.git("update-ref", "--create-reflog", checkpointRef(branch), parent)
		f.git("update-ref", "-m", "latest", checkpointRef(branch), tip)
		if i%2 == 0 {
			f.git("update-ref", "refs/heads/"+branch, head)
		}
	}
	log := refReadWrapper(t, f, r, "")
	assertUnchanged := preserveReadFixture(t, f)
	refs, err := r.Refs(context.Background())
	if err != nil || len(refs) != 24 {
		t.Fatalf("refs = %+v, %v", refs, err)
	}
	for i, ref := range refs {
		if ref.Ref != fmt.Sprintf("refs/wip/topic/%02d", i) || ref.Base != head || ref.Orphaned != (i%2 != 0) {
			t.Fatalf("ref %d = %+v", i, ref)
		}
	}
	commands, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if count := len(strings.Split(strings.TrimSpace(string(commands)), "\n")); count > 7 {
		t.Fatalf("Refs spawned %d processes for 24 noninitial refs:\n%s", count, commands)
	}
	if err := os.WriteFile(log, nil, 0600); err != nil {
		t.Fatal(err)
	}
	entries, err := r.Reflog(context.Background(), refs[0].Ref, 1)
	if err != nil || len(entries) != 1 || entries[0].Commit != refs[0].Tip || entries[0].Selector != refs[0].Ref+"@{0}" || entries[0].Message != "latest" {
		t.Fatalf("limited reflog = %+v, %v", entries, err)
	}
	commands, err = os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if count := len(strings.Split(strings.TrimSpace(string(commands)), "\n")); count > 9 {
		t.Fatalf("Reflog spawned %d processes:\n%s", count, commands)
	}
	assertUnchanged()
	f.git("update-ref", "-d", refs[0].Ref)
	updated, err := r.Refs(context.Background())
	if err != nil || len(updated) != 23 || updated[0].Ref != refs[1].Ref {
		t.Fatalf("fresh refs = %+v, %v", updated, err)
	}
}

func TestRefReadsRejectInvalidStorage(t *testing.T) {
	for _, scenario := range []string{"unowned", "symbolic", "broken", "bad-name", "unrelated-broken", "missing-base", "missing-parent", "parent-missing-base", "parent-missing-parent", "wrong-parent-stack", "wrong-tip-branch", "tag"} {
		t.Run(scenario, func(t *testing.T) {
			f := newTestRepository(t, true)
			r := openFixture(t, f)
			head, tree := f.git("rev-parse", "HEAD"), f.git("rev-parse", "HEAD^{tree}")
			ref, tip := "refs/wip/main", ""
			missing := strings.Repeat("b", len(head))
			makeRaw := func(parent, branch, base string) string {
				raw := "tree " + tree + "\nparent " + parent + "\nauthor Fixture <fixture@example.invalid> 1 +0000\ncommitter Fixture <fixture@example.invalid> 1 +0000\n\n" + checkpointBody(t, branch, base, "checkpoint")
				return hashFixtureObject(t, f, "commit", raw)
			}
			switch scenario {
			case "unowned":
				tip = head
			case "symbolic":
				f.git("symbolic-ref", ref, "refs/heads/main")
			case "broken":
				f.write(".git/refs/wip/main", missing+"\n")
			case "bad-name":
				f.write(".git/refs/wip/bad..name", head+"\n")
				ref = "refs/wip/bad..name"
			case "unrelated-broken":
				tip = makeRaw(head, "main", head)
				f.write(".git/refs/heads/broken", missing+"\n")
			case "missing-base":
				tip = makeRaw(head, "main", missing)
			case "missing-parent":
				tip = makeRaw(missing, "main", head)
			case "parent-missing-base":
				parent := makeRaw(head, "main", missing)
				tip = makeRaw(parent, "main", missing)
			case "parent-missing-parent":
				parent := makeRaw(missing, "main", head)
				tip = makeRaw(parent, "main", head)
			case "wrong-parent-stack":
				parent := makeRaw(head, "other", head)
				tip = makeRaw(parent, "main", head)
			case "wrong-tip-branch":
				tip = makeRaw(head, "other", head)
			case "tag":
				f.git("tag", "-a", "-m", "tag", "checkpoint", makeRaw(head, "main", head))
				tip = f.git("rev-parse", "refs/tags/checkpoint")
			}
			if tip != "" {
				f.git("update-ref", ref, tip)
			}
			_, err := r.Refs(context.Background())
			requireErrorKind(t, err, KindInvalidRef)
			_, err = r.Reflog(context.Background(), ref, 1)
			requireErrorKind(t, err, KindInvalidRef)
		})
	}
}

func TestRefReadsRejectMalformedGitCommitHeaders(t *testing.T) {
	for _, dependency := range []string{"tip", "base", "parent", "parent-base", "parent-parent"} {
		t.Run(dependency, func(t *testing.T) {
			f := newTestRepository(t, true)
			r := openFixture(t, f)
			head, tree := f.git("rev-parse", "HEAD"), f.git("rev-parse", "HEAD^{tree}")
			// Literal objects are needed because hash-object normally rejects this
			// malformed commit. Its checkpoint envelope alone remains well formed.
			raw := "parent " + head + "\nauthor Fixture <fixture@example.invalid> 1 +0000\ncommitter Fixture <fixture@example.invalid> 1 +0000\n\n" + checkpointBody(t, "main", head, "missing tree header")
			cmd := exec.Command(f.gitPath, "hash-object", "--literally", "-w", "-t", "commit", "--stdin")
			cmd.Dir, cmd.Env, cmd.Stdin = f.dir, f.env, strings.NewReader(raw)
			out, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			malformed := strings.TrimSpace(string(out))
			checkpoint := func(parent, base string) string {
				raw := "tree " + tree + "\nparent " + parent + "\nauthor Fixture <fixture@example.invalid> 1 +0000\ncommitter Fixture <fixture@example.invalid> 1 +0000\n\n" + checkpointBody(t, "main", base, "checkpoint")
				return hashFixtureObject(t, f, "commit", raw)
			}
			var tip string
			switch dependency {
			case "tip":
				tip = malformed
			case "base":
				tip = checkpoint(head, malformed)
			case "parent":
				tip = checkpoint(malformed, head)
			case "parent-base":
				parent := checkpoint(head, malformed)
				tip = checkpoint(parent, malformed)
			case "parent-parent":
				parent := checkpoint(malformed, head)
				tip = checkpoint(parent, head)
			}
			f.write(".git/refs/wip/main", tip+"\n")
			// Match the established ordinary reader's strict Git parsing.
			_, err = r.Status(context.Background())
			requireErrorKind(t, err, KindInvalidRef)
			_, err = r.Refs(context.Background())
			requireErrorKind(t, err, KindInvalidRef)
			_, err = r.Reflog(context.Background(), "refs/wip/main", 1)
			requireErrorKind(t, err, KindInvalidRef)
		})
	}
}

func TestRefReadsGitValidationDoesNotTraverseAncestry(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	tree := f.git("rev-parse", "HEAD^{tree}")
	missing := strings.Repeat("b", 40)
	// Ordinary dependency commits need valid Git headers, but their unvisited
	// ancestry remains outside the existing bounded tip/parent validation.
	raw := "tree " + tree + "\nparent " + missing + "\nauthor Fixture <fixture@example.invalid> 1 +0000\ncommitter Fixture <fixture@example.invalid> 1 +0000\n\nordinary base with unavailable ancestor\n"
	base := hashFixtureObject(t, f, "commit", raw)
	tip := commitWithRawMessage(t, f, base, checkpointBody(t, "main", base, "checkpoint"))
	f.git("update-ref", "--create-reflog", "refs/wip/main", tip)
	refs, err := r.Refs(context.Background())
	if err != nil || len(refs) != 1 || refs[0].Tip != tip || refs[0].Base != base {
		t.Fatalf("bounded refs = %+v, %v", refs, err)
	}
	entries, err := r.Reflog(context.Background(), "refs/wip/main", 1)
	if err != nil || len(entries) != 1 || entries[0].Commit != tip {
		t.Fatalf("bounded reflog = %+v, %v", entries, err)
	}
}

func TestRefReadsOutputLimitsAndCancellation(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	cp := captureFixture(t, r, CaptureOptions{Baseline: true, Message: strings.Repeat("large ", 2048)})
	bounded, err := OpenWithOptions(context.Background(), f.dir, OpenOptions{MaxStdoutBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	for _, read := range []func(context.Context) error{
		func(ctx context.Context) error { _, err := bounded.Refs(ctx); return err },
		func(ctx context.Context) error { _, err := bounded.Reflog(ctx, cp.Ref, 1); return err },
	} {
		requireErrorKind(t, read(context.Background()), KindOutputLimit)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := read(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled ref read = %v", err)
		}
	}
}

func TestRefReadsSHA256(t *testing.T) {
	f := newTestRepository(t, false)
	if err := os.RemoveAll(filepath.Join(f.dir, ".git")); err != nil {
		t.Fatal(err)
	}
	f.git("init", "--object-format=sha256", "--initial-branch=main", "--template=")
	f.write("tracked.txt", "SHA-256 content\n")
	f.git("add", ".")
	f.git("commit", "-m", "SHA-256 base")
	r := openFixture(t, f)
	captureFixture(t, r, CaptureOptions{Baseline: true})
	cp := captureFixture(t, r, CaptureOptions{Baseline: true})
	refs, err := r.Refs(context.Background())
	if err != nil || len(refs) != 1 || refs[0].Tip != cp.Commit || len(refs[0].Tip) != 64 {
		t.Fatalf("SHA-256 refs = %+v, %v", refs, err)
	}
	entries, err := r.Reflog(context.Background(), cp.Ref, 0)
	if err != nil || len(entries) != 2 || entries[0].Commit != cp.Commit {
		t.Fatalf("SHA-256 reflog = %+v, %v", entries, err)
	}
}

func TestReflogRejectsConcurrentRefChange(t *testing.T) {
	for _, window := range []string{"before-log", "after-log"} {
		for _, replacement := range []string{"move", "delete", "symbolic"} {
			t.Run(window+"/"+replacement, func(t *testing.T) {
				f := newTestRepository(t, true)
				r := openFixture(t, f)
				cp := captureFixture(t, r, CaptureOptions{Baseline: true})
				mutation := "update-ref " + cp.Ref + " HEAD"
				if replacement == "delete" {
					mutation = "update-ref -d " + cp.Ref
				} else if replacement == "symbolic" {
					mutation = "symbolic-ref " + cp.Ref + " refs/heads/main"
				}
				command := "for-each-ref"
				if window == "before-log" {
					command = "reflog"
				}
				before := "for arg; do\nif test \"$arg\" = " + command + "; then\n" + "'" + f.gitPath + "' -C '" + f.dir + "' " + mutation + "\nfi\ndone"
				refReadWrapper(t, f, r, before)
				entries, err := r.Reflog(context.Background(), cp.Ref, 0)
				requireErrorKind(t, err, KindConflict)
				if len(entries) != 0 {
					t.Fatalf("conflicted log returned data: %+v", entries)
				}
			})
		}
	}
}

func TestReflogReadErrorsPreserveFailureKind(t *testing.T) {
	for _, limited := range []bool{false, true} {
		t.Run(fmt.Sprintf("output-limit=%t", limited), func(t *testing.T) {
			f := newTestRepository(t, true)
			r := openFixture(t, f)
			cp := captureFixture(t, r, CaptureOptions{Baseline: true})
			failure := "printf 'fixture reflog failure\\n' >&2\nexit 17"
			kind := KindGit
			if limited {
				r.maxStdoutBytes = 2048
				// Even a concurrent deletion must not turn an output cap into a conflict.
				failure = "'" + f.gitPath + "' -C '" + f.dir + "' update-ref -d " + cp.Ref + "\nprintf '%03000d' 0\nexit 17"
				kind = KindOutputLimit
			}
			before := "for arg; do\nif test \"$arg\" = reflog; then\n" + failure + "\nfi\ndone"
			log := refReadWrapper(t, f, r, before)
			entries, err := r.Reflog(context.Background(), cp.Ref, 0)
			requireErrorKind(t, err, kind)
			if !limited && !strings.Contains(err.Error(), "fixture reflog failure") {
				t.Fatalf("original reflog failure was replaced: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("failed log returned data: %+v", entries)
			}
			commands, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if rechecked := strings.Contains(string(commands), "for-each-ref"); rechecked == limited {
				t.Fatalf("unexpected ref recheck after failure (limited=%t): %s", limited, commands)
			}
		})
	}
}

func TestReflogReadErrorSurvivesFailedRecheck(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	cp := captureFixture(t, r, CaptureOptions{Baseline: true})
	before := "for arg; do\ncase \"$arg\" in\nreflog) printf 'fixture read failure\\n' >&2; exit 17;;\nfor-each-ref) printf 'fixture recheck failure\\n' >&2; exit 18;;\nesac\ndone"
	refReadWrapper(t, f, r, before)
	entries, err := r.Reflog(context.Background(), cp.Ref, 0)
	requireErrorKind(t, err, KindGit)
	if len(entries) != 0 || !strings.Contains(err.Error(), "fixture read failure") || strings.Contains(err.Error(), "fixture recheck failure") {
		t.Fatalf("original reflog failure was replaced: %+v %v", entries, err)
	}
}

func TestReflogCancellationDuringRead(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	cp := captureFixture(t, r, CaptureOptions{Baseline: true})
	ready := filepath.Join(t.TempDir(), "ready")
	before := "for arg; do\nif test \"$arg\" = reflog; then\n" + "'" + f.gitPath + "' -C '" + f.dir + "' update-ref -d " + cp.Ref + "\n: > '" + ready + "'\nexec sleep 30\nfi\ndone"
	log := refReadWrapper(t, f, r, before)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := r.Reflog(ctx, cp.Ref, 0)
		done <- err
	}()
	waitForCaptureFilter(t, ready)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled reflog read = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reflog did not finish promptly after cancellation")
	}
	commands, err := os.ReadFile(log)
	if err != nil || strings.Contains(string(commands), "for-each-ref") {
		t.Fatalf("cancelled read attempted ref recheck: %s %v", commands, err)
	}
}

func TestRefReadsCancellationDuringValidation(t *testing.T) {
	for _, operation := range []string{"refs", "reflog", "refs-parse", "reflog-parse"} {
		t.Run(operation, func(t *testing.T) {
			f := newTestRepository(t, true)
			r := openFixture(t, f)
			cp := captureFixture(t, r, CaptureOptions{Baseline: true})
			ready := filepath.Join(t.TempDir(), "ready")
			command := "cat-file"
			if strings.HasSuffix(operation, "-parse") {
				command = "rev-list"
			}
			before := "for arg; do\nif test \"$arg\" = " + command + "; then\n: > '" + ready + "'\nexec sleep 30\nfi\ndone"
			refReadWrapper(t, f, r, before)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				if strings.HasPrefix(operation, "refs") {
					_, err := r.Refs(ctx)
					done <- err
				} else {
					_, err := r.Reflog(ctx, cp.Ref, 1)
					done <- err
				}
			}()
			waitForCaptureFilter(t, ready)
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("active batch cancellation = %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("ref read did not finish promptly after cancellation")
			}
		})
	}
}
