package wip

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDiscoverStacksSkipsOrdinaryAncestryAndClassifiesAllRoots(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	base, tree := f.git("rev-parse", "HEAD"), f.git("rev-parse", "HEAD^{tree}")
	old := commitWithRawMessage(t, f, base, checkpointBody(t, "main", base, "old explicit checkpoint"))
	for i := 0; i < 600; i++ {
		base = f.git("commit-tree", tree, "-p", base, "-m", fmt.Sprintf("ordinary %d", i))
	}
	cp := commitWithRawMessage(t, f, base, checkpointBody(t, "main", base, "new stack"))
	defer preserveReadFixture(t, f)()
	got, err := r.DiscoverStacks(context.Background(), StackDiscoveryOptions{Roots: []string{cp, base, old, cp}, CheckpointLimit: 1})
	if err != nil || got.Coverage.Stop != "complete" || !got.Coverage.Exhausted || !got.Coverage.RootsComplete || got.Visited != 0 || len(got.Checkpoints) != 2 || len(got.Failures) != 0 || got.Continuation != nil {
		t.Fatalf("sparse stacks = %+v, %v", got, err)
	}
	want := []RootClassification{{cp, "checkpoint"}, {base, "noncheckpoint"}, {old, "checkpoint"}}
	if !reflect.DeepEqual(got.Roots, want) {
		t.Fatalf("roots = %+v, want %+v", got.Roots, want)
	}
}

func TestDiscoverStacksOverlappingRootsAndContinuation(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	base := f.git("rev-parse", "HEAD")
	tip := base
	chain := []string{}
	for i := 0; i < 11; i++ {
		tip = commitWithRawMessage(t, f, tip, checkpointBody(t, "main", base, fmt.Sprintf("checkpoint %d", i)))
		chain = append(chain, tip)
	}
	defer preserveReadFixture(t, f)()
	allRoots := append(append([]string{}, chain...), base)
	all, err := r.DiscoverStacks(context.Background(), StackDiscoveryOptions{Roots: allRoots, CheckpointLimit: 1})
	if err != nil || all.Visited != 0 || len(all.Checkpoints) != 11 || all.Coverage.Stop != "complete" {
		t.Fatalf("root chains = %+v, %v", all, err)
	}
	page, err := r.DiscoverStacks(context.Background(), StackDiscoveryOptions{Roots: []string{tip, chain[7], tip}, CheckpointLimit: 2})
	seen := map[string]bool{}
	totalVisited, pages := 0, 0
	for {
		if err != nil {
			t.Fatal(err)
		}
		for _, cp := range page.Checkpoints {
			if seen[cp.Commit] {
				t.Fatalf("repeated checkpoint %s", cp.Commit)
			}
			seen[cp.Commit] = true
		}
		totalVisited += page.Visited
		pages++
		if page.Continuation == nil {
			if page.Coverage.Stop != "complete" || !page.Coverage.Exhausted {
				t.Fatalf("last page = %+v", page)
			}
			break
		}
		if page.Coverage.Stop != "budget" || page.Visited > 2 || pages > 12 {
			t.Fatalf("page = %+v", page)
		}
		cursor := page.Continuation
		first, firstErr := r.DiscoverStacks(context.Background(), StackDiscoveryOptions{Continuation: cursor, CheckpointLimit: 2})
		second, secondErr := r.DiscoverStacks(context.Background(), StackDiscoveryOptions{Continuation: cursor, CheckpointLimit: 2})
		if firstErr != nil || secondErr != nil || !reflect.DeepEqual(first, second) {
			t.Fatalf("cursor reuse mutated state: %+v / %+v, %v / %v", first, second, firstErr, secondErr)
		}
		page, err = first, firstErr
	}
	if len(seen) != 11 || totalVisited != 9 || pages < 3 {
		t.Fatalf("pagination found %d checkpoints over %d candidates/%d pages", len(seen), totalVisited, pages)
	}
}

func TestDiscoverStacksIntegrityBoundaries(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	base := f.git("rev-parse", "HEAD")
	good := commitWithRawMessage(t, f, base, checkpointBody(t, "main", base, "older valid"))
	malformed := commitWithRawMessage(t, f, good, "message\n\ngo-git-wip:99 {}\n")
	bridge := commitWithRawMessage(t, f, malformed, checkpointBody(t, "main", base, "invalid immediate parent"))
	tip := commitWithRawMessage(t, f, bridge, checkpointBody(t, "main", base, "locally valid root"))
	wrong := commitWithRawMessage(t, f, good, checkpointBody(t, "other", base, "wrong stack"))
	ordinary := commitWithRawMessage(t, f, good, "ordinary intermediary")
	unowned := commitWithRawMessage(t, f, ordinary, checkpointBody(t, "main", base, "ordinary parent"))
	missing := strings.Repeat("a", len(base))
	badBase := commitWithRawMessage(t, f, base, checkpointBody(t, "main", missing, "unavailable base"))
	blob := f.git("rev-parse", "HEAD:tracked.txt")
	defer preserveReadFixture(t, f)()
	got, err := r.DiscoverStacks(context.Background(), StackDiscoveryOptions{Roots: []string{tip, wrong, unowned, badBase, blob, missing}, CheckpointLimit: 20})
	if err != nil || len(got.Checkpoints) != 1 || got.Checkpoints[0].Commit != tip || len(got.Failures) != 6 || got.Coverage.Stop != "failure" || !got.Coverage.Exhausted || !got.Coverage.RootsComplete {
		t.Fatalf("invalid boundary = %+v, %v", got, err)
	}
	want := []RootClassification{{tip, "checkpoint"}, {wrong, "invalid"}, {unowned, "invalid"}, {badBase, "invalid"}, {blob, "unavailable"}, {missing, "unavailable"}}
	if !reflect.DeepEqual(got.Roots, want) {
		t.Fatalf("classification = %+v", got.Roots)
	}
	for _, failure := range got.Failures {
		if failure.Err == nil || failure.Message == "" {
			t.Fatalf("failure = %+v", failure)
		}
	}
}

func TestDiscoverStacksMissingParentAndFailureCoverage(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	base, tree := f.git("rev-parse", "HEAD"), f.git("rev-parse", "HEAD^{tree}")
	missing := strings.Repeat("b", len(base))
	raw := "tree " + tree + "\nparent " + missing + "\nauthor Fixture <fixture@example.invalid> 1 +0000\ncommitter Fixture <fixture@example.invalid> 1 +0000\n\n" + checkpointBody(t, "main", base, "missing parent")
	cmd := exec.Command(f.gitPath, "hash-object", "-w", "-t", "commit", "--stdin")
	cmd.Dir, cmd.Env, cmd.Stdin = f.dir, f.env, strings.NewReader(raw)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	bad := strings.TrimSpace(string(out))
	tip := base
	for i := 0; i < 5; i++ {
		tip = commitWithRawMessage(t, f, tip, checkpointBody(t, "main", base, fmt.Sprintf("valid %d", i)))
	}
	defer preserveReadFixture(t, f)()
	got, err := r.DiscoverStacks(context.Background(), StackDiscoveryOptions{Roots: []string{bad, tip}, CheckpointLimit: 1})
	if err != nil || len(got.Failures) != 1 || got.Roots[0].Kind != "invalid" || got.Coverage.Stop != "budget" || got.Continuation == nil || !got.Coverage.RootsComplete {
		t.Fatalf("independent coverage = %+v, %v", got, err)
	}
	for got.Continuation != nil {
		got, err = r.DiscoverStacks(context.Background(), StackDiscoveryOptions{Continuation: got.Continuation, CheckpointLimit: 2})
		if err != nil {
			t.Fatal(err)
		}
	}
	if got.Coverage.Stop != "failure" || !got.Coverage.Exhausted {
		t.Fatalf("earlier integrity failure lost = %+v", got)
	}
}

func TestDiscoverStacksContinuationValidationAndOutputLimits(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	first := captureFixture(t, r, CaptureOptions{Baseline: true})
	second := captureFixture(t, r, CaptureOptions{Baseline: true})
	third := captureFixture(t, r, CaptureOptions{Baseline: true, Message: strings.Repeat("large ", 2048)})
	defer preserveReadFixture(t, f)()
	page, err := r.DiscoverStacks(context.Background(), StackDiscoveryOptions{Roots: []string{third.Commit}, CheckpointLimit: 1})
	if err != nil || page.Continuation == nil {
		t.Fatalf("first page = %+v, %v", page, err)
	}
	other := openFixture(t, newTestRepository(t, true))
	_, err = other.DiscoverStacks(context.Background(), StackDiscoveryOptions{Continuation: page.Continuation, CheckpointLimit: 1})
	requireErrorKind(t, err, KindInvalidOption)
	for _, options := range []StackDiscoveryOptions{{CheckpointLimit: 0}, {CheckpointLimit: -1}, {Roots: []string{"HEAD"}, CheckpointLimit: 1}, {Roots: []string{third.Commit[:8]}, CheckpointLimit: 1}, {Roots: []string{third.Commit}, Continuation: page.Continuation, CheckpointLimit: 1}, {Continuation: &StackContinuation{}, CheckpointLimit: 1}} {
		_, err = r.DiscoverStacks(context.Background(), options)
		requireErrorKind(t, err, KindInvalidOption)
	}
	bounded, err := OpenWithOptions(context.Background(), f.dir, OpenOptions{MaxStdoutBytes: 512, MaxStderrBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	got, err := bounded.DiscoverStacks(context.Background(), StackDiscoveryOptions{Roots: []string{third.Commit}, CheckpointLimit: 1})
	requireErrorKind(t, err, KindOutputLimit)
	if got.Coverage.Stop != "failure" || got.Continuation != nil || len(got.Checkpoints) != 0 {
		t.Fatalf("overflow = %+v, %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err = r.DiscoverStacks(ctx, StackDiscoveryOptions{Roots: []string{first.Commit, second.Commit}, CheckpointLimit: 1})
	if !errors.Is(err, context.Canceled) || got.Coverage.Stop != "failure" || got.Continuation != nil {
		t.Fatalf("cancel = %+v, %v", got, err)
	}
}

func TestDiscoverStacksRootStateLimit(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	roots := make([]string, stackRootLimit+1)
	for i := range roots {
		roots[i] = fmt.Sprintf("%040x", i+1)
	}
	got, err := r.DiscoverStacks(context.Background(), StackDiscoveryOptions{Roots: roots, CheckpointLimit: 1})
	if err != nil || len(got.Roots) != stackRootLimit || got.Coverage.RootsComplete || got.Coverage.Exhausted || got.Coverage.Stop != "state_limit" || got.Continuation != nil {
		t.Fatalf("root state limit = %+v, %v", got.Coverage, err)
	}
}

func TestDiscoverStacksBatchProcessCount(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	base, tip := f.git("rev-parse", "HEAD"), f.git("rev-parse", "HEAD")
	for i := 0; i < 30; i++ {
		tip = commitWithRawMessage(t, f, tip, checkpointBody(t, "main", base, fmt.Sprintf("checkpoint %d", i)))
	}
	dir := t.TempDir()
	log, wrapper := filepath.Join(dir, "commands"), filepath.Join(dir, "git")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + quote(log) + "\nexec " + quote(f.gitPath) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	r.gitPath = wrapper
	defer preserveReadFixture(t, f)()
	got, err := r.DiscoverStacks(context.Background(), StackDiscoveryOptions{Roots: []string{tip, base}, CheckpointLimit: 40})
	if err != nil || len(got.Checkpoints) != 30 || got.Visited != 29 || got.Coverage.Stop != "complete" {
		t.Fatalf("batched stacks = %+v, %v", got, err)
	}
	commands, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if count := len(strings.Split(strings.TrimSpace(string(commands)), "\n")); count > 8 {
		t.Fatalf("%d processes for 30 checkpoints:\n%s", count, commands)
	}
}

func TestDiscoverStacksCancellationDuringRootBatch(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	base := f.git("rev-parse", "HEAD")
	dir := t.TempDir()
	ready, wrapper := filepath.Join(dir, "ready"), filepath.Join(dir, "git")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nfor arg; do\nif test \"$arg\" = cat-file; then\n: > " + quote(ready) + "\nexec sleep 30\nfi\ndone\nexec " + quote(f.gitPath) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	r.gitPath = wrapper
	defer preserveReadFixture(t, f)()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		result StackDiscoveryResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := r.DiscoverStacks(ctx, StackDiscoveryOptions{Roots: []string{base}, CheckpointLimit: 1})
		done <- outcome{result, err}
	}()
	waitForCaptureFilter(t, ready)
	cancel()
	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) || got.result.Coverage.Stop != "failure" || got.result.Continuation != nil {
			t.Fatalf("active cancellation = %+v, %v", got.result, got.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("active cancellation did not finish")
	}
}

func TestDiscoverStacksBaseExclusionIsLocalToEachStack(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	base := f.git("rev-parse", "HEAD")
	first := commitWithRawMessage(t, f, base, checkpointBody(t, "main", base, "first"))
	second := commitWithRawMessage(t, f, first, checkpointBody(t, "main", base, "second"))
	tip := commitWithRawMessage(t, f, second, checkpointBody(t, "main", base, "third"))
	other := commitWithRawMessage(t, f, second, checkpointBody(t, "other", second, "checkpoint-shaped base"))
	defer preserveReadFixture(t, f)()
	got, err := r.DiscoverStacks(context.Background(), StackDiscoveryOptions{Roots: []string{tip, other}, CheckpointLimit: 10})
	if err != nil || len(got.Checkpoints) != 4 || got.Visited != 2 || got.Coverage.Stop != "complete" {
		t.Fatalf("cross-stack base exclusion = %+v, %v", got, err)
	}
}

func TestDiscoverStacksAncestryCommandFailureKeepsSafeRoots(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	base := f.git("rev-parse", "HEAD")
	first := commitWithRawMessage(t, f, base, checkpointBody(t, "main", base, "first"))
	tip := commitWithRawMessage(t, f, first, checkpointBody(t, "main", base, "second"))
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "git")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nfor arg; do\nif test \"$arg\" = rev-list; then\nexit 1\nfi\ndone\nexec " + quote(f.gitPath) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	r.gitPath = wrapper
	defer preserveReadFixture(t, f)()
	got, err := r.DiscoverStacks(context.Background(), StackDiscoveryOptions{Roots: []string{tip}, CheckpointLimit: 1})
	requireErrorKind(t, err, KindGit)
	if !got.Coverage.RootsComplete || got.Coverage.Stop != "failure" || got.Coverage.Exhausted || len(got.Checkpoints) != 1 || got.Checkpoints[0].Commit != tip || got.Continuation != nil {
		t.Fatalf("partial command failure = %+v, %v", got, err)
	}
}

func TestDiscoverStacksContinuationStateLimit(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	base := f.git("rev-parse", "HEAD")
	first := commitWithRawMessage(t, f, base, checkpointBody(t, "main", base, "first"))
	cursor := &StackContinuation{root: r.root, gitDir: r.gitDir, length: len(base), expanded: map[string]bool{}, frontier: []stackFrontier{{first, "main", base}}}
	for i := 0; i < stackStateLimit-1; i++ {
		cursor.expanded[fmt.Sprintf("%040x", i+1)] = true
	}
	got, err := r.DiscoverStacks(context.Background(), StackDiscoveryOptions{Continuation: cursor, CheckpointLimit: 10})
	if err != nil || got.Coverage.Stop != "state_limit" || got.Coverage.Exhausted || got.Continuation != nil || got.Visited != 0 {
		t.Fatalf("continuation state limit = %+v, %v", got, err)
	}
	if len(cursor.expanded) != stackStateLimit-1 || len(cursor.frontier) != 1 {
		t.Fatal("original cursor mutated")
	}
}

func TestDiscoverStacksSHA256(t *testing.T) {
	f := newTestRepository(t, false)
	if err := os.RemoveAll(filepath.Join(f.dir, ".git")); err != nil {
		t.Fatal(err)
	}
	f.git("init", "--object-format=sha256", "--initial-branch=main", "--template=")
	f.write("tracked.txt", "SHA-256 content\n")
	f.git("add", ".")
	f.git("commit", "-m", "base")
	r := openFixture(t, f)
	first := captureFixture(t, r, CaptureOptions{Baseline: true})
	tip := captureFixture(t, r, CaptureOptions{Baseline: true})
	defer preserveReadFixture(t, f)()
	got, err := r.DiscoverStacks(context.Background(), StackDiscoveryOptions{Roots: []string{tip.Commit}, CheckpointLimit: 1})
	if err != nil || got.Coverage.Stop != "complete" || len(got.Checkpoints) != 2 || got.Checkpoints[1].Commit != first.Commit || len(got.Checkpoints[0].Commit) != 64 {
		t.Fatalf("SHA-256 stacks = %+v, %v", got, err)
	}
}
