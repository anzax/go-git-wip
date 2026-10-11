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

func checkpointBody(t *testing.T, branch, base, message string) string {
	t.Helper()
	body, err := encodeCheckpointMessage(branch, base, message)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestDiscoverOverlappingRootsAndParentLookahead(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	first := captureFixture(t, r, CaptureOptions{Baseline: true, Message: "first\n\nbody\n"})
	second := captureFixture(t, r, CaptureOptions{Baseline: true, Message: "second"})
	defer preserveReadFixture(t, f)()
	result, err := r.Discover(context.Background(), DiscoveryOptions{Roots: []string{second.Commit, first.Commit, second.Commit, first.HEAD}, CandidateLimit: 20})
	if err != nil || result.Incomplete || result.Visited != 3 || len(result.Checkpoints) != 2 || len(result.Failures) != 0 {
		t.Fatalf("overlapping roots = %+v, %v", result, err)
	}
	byID := make(map[string]Checkpoint)
	for _, cp := range result.Checkpoints {
		byID[cp.Commit] = cp
	}
	if len(byID) != 2 || byID[first.Commit].Message != "first\n\nbody\n" || byID[second.Commit].Parent != first.Commit {
		t.Fatalf("decoded checkpoints = %+v", byID)
	}
	limited, err := r.Discover(context.Background(), DiscoveryOptions{Roots: []string{second.Commit}, CandidateLimit: 1})
	if err != nil || !limited.Incomplete || limited.Visited != 1 || len(limited.Checkpoints) != 1 || limited.Checkpoints[0].Commit != second.Commit || len(limited.Failures) != 0 {
		t.Fatalf("parent lookahead outside candidate window = %+v, %v", limited, err)
	}
	exact, err := r.Discover(context.Background(), DiscoveryOptions{Roots: []string{second.Commit}, CandidateLimit: 3})
	if err != nil || exact.Incomplete || exact.Visited != 3 || len(exact.Checkpoints) != 2 {
		t.Fatalf("exact exhausted budget = %+v, %v", exact, err)
	}
}

func TestDiscoverSparseHistoryCountsUnrelatedCommits(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	head, tree := f.git("rev-parse", "HEAD"), f.git("rev-parse", "HEAD^{tree}")
	old := commitWithRawMessage(t, f, head, checkpointBody(t, "main", head, "old checkpoint"))
	tip := old
	for i := 0; i < 16; i++ {
		tip = f.git("commit-tree", tree, "-p", tip, "-m", fmt.Sprintf("ordinary %d", i))
	}
	defer preserveReadFixture(t, f)()
	result, err := r.Discover(context.Background(), DiscoveryOptions{Roots: []string{tip}, CandidateLimit: 5})
	if err != nil || !result.Incomplete || result.Visited != 5 || len(result.Checkpoints) != 0 || len(result.Failures) != 0 {
		t.Fatalf("sparse bounded scan = %+v, %v", result, err)
	}
	result, err = r.Discover(context.Background(), DiscoveryOptions{Roots: []string{tip}, CandidateLimit: 30})
	if err != nil || result.Incomplete || result.Visited != 18 || len(result.Checkpoints) != 1 || result.Checkpoints[0].Commit != old {
		t.Fatalf("sparse exhausted scan = %+v, %v", result, err)
	}
}

func TestDiscoverMalformedMissingAndOrdinaryRoots(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	head := f.git("rev-parse", "HEAD")
	good := commitWithRawMessage(t, f, head, checkpointBody(t, "main", head, "good"))
	legacy := commitWithRawMessage(t, f, good, fmt.Sprintf("go-git-wip:1 {\"branch\":\"main\",\"base\":\"%s\"}\n\nlegacy\n", head))
	malformed := commitWithRawMessage(t, f, head, "message\n\ngo-git-wip:99 {}\n")
	wrongStack := commitWithRawMessage(t, f, good, checkpointBody(t, "different", head, "wrong parent branch"))
	unavailable := strings.Repeat("a", len(head))
	badBase := commitWithRawMessage(t, f, head, checkpointBody(t, "main", unavailable, "unavailable base"))
	blob := f.git("rev-parse", "HEAD:tracked.txt")
	defer preserveReadFixture(t, f)()
	result, err := r.Discover(context.Background(), DiscoveryOptions{Roots: []string{legacy, malformed, wrongStack, badBase, unavailable, unavailable, blob, head}, CandidateLimit: 30})
	if err != nil || !result.Incomplete || result.Visited != 6 || len(result.Checkpoints) != 2 || len(result.Failures) != 5 {
		t.Fatalf("mixed roots = %+v, %v", result, err)
	}
	failures := make(map[string]error)
	for _, failure := range result.Failures {
		failures[failure.Commit] = failure.Err
		if failure.Message == "" || failure.Err == nil {
			t.Fatalf("failure is not explicit: %+v", failure)
		}
	}
	for _, oid := range []string{malformed, wrongStack, badBase} {
		requireErrorKind(t, failures[oid], KindInvalidRef)
	}
	for _, oid := range []string{unavailable, blob} {
		requireErrorKind(t, failures[oid], KindNotFound)
	}
}

func TestDiscoverMalformedImmediateParentIsNotTrusted(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	head := f.git("rev-parse", "HEAD")
	parent := commitWithRawMessage(t, f, head, "ordinary parent\n")
	tip := commitWithRawMessage(t, f, parent, checkpointBody(t, "main", head, "invalid stack"))
	defer preserveReadFixture(t, f)()
	result, err := r.Discover(context.Background(), DiscoveryOptions{Roots: []string{tip}, CandidateLimit: 1})
	if err != nil || !result.Incomplete || result.Visited != 1 || len(result.Checkpoints) != 0 || len(result.Failures) != 1 || result.Failures[0].Commit != tip {
		t.Fatalf("unowned parent outside window = %+v, %v", result, err)
	}
	requireErrorKind(t, result.Failures[0].Err, KindInvalidRef)
}

func TestDiscoverMissingAncestorReturnsExplicitError(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	head := f.git("rev-parse", "HEAD")
	tree := f.git("rev-parse", "HEAD^{tree}")
	missing := strings.Repeat("b", len(head))
	raw := "tree " + tree + "\nparent " + missing + "\nauthor Fixture <fixture@example.invalid> 1 +0000\ncommitter Fixture <fixture@example.invalid> 1 +0000\n\n" + checkpointBody(t, "main", head, "missing parent")
	cmd := exec.Command(f.gitPath, "hash-object", "-w", "-t", "commit", "--stdin")
	cmd.Dir, cmd.Env, cmd.Stdin = f.dir, f.env, strings.NewReader(raw)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	tip := strings.TrimSpace(string(out))
	defer preserveReadFixture(t, f)()
	result, err := r.Discover(context.Background(), DiscoveryOptions{Roots: []string{tip}, CandidateLimit: 5})
	if err == nil || !result.Incomplete || len(result.Checkpoints) != 0 {
		t.Fatalf("missing reachable object was hidden: %+v, %v", result, err)
	}
	requireErrorKind(t, err, KindGit)
}

func TestDiscoverOutputBoundCancellationAndInvalidOptions(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	cp := captureFixture(t, r, CaptureOptions{Baseline: true, Message: strings.Repeat("large ", 2048)})
	bounded, err := OpenWithOptions(context.Background(), f.dir, OpenOptions{MaxStdoutBytes: 512, MaxStderrBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	defer preserveReadFixture(t, f)()
	result, err := bounded.Discover(context.Background(), DiscoveryOptions{Roots: []string{cp.Commit}, CandidateLimit: 5})
	requireErrorKind(t, err, KindOutputLimit)
	if !result.Incomplete || len(result.Checkpoints) != 0 {
		t.Fatalf("batch output overflow = %+v, %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = bounded.Discover(ctx, DiscoveryOptions{Roots: []string{cp.Commit}, CandidateLimit: 5})
	if !errors.Is(err, context.Canceled) || !result.Incomplete {
		t.Fatalf("discovery cancellation = %+v, %v", result, err)
	}
	for _, options := range []DiscoveryOptions{{CandidateLimit: 0}, {CandidateLimit: -1}, {Roots: []string{"HEAD"}, CandidateLimit: 5}, {Roots: []string{cp.Commit[:8]}, CandidateLimit: 5}, {Roots: []string{strings.ToUpper(cp.Commit)}, CandidateLimit: 5}} {
		_, err := r.Discover(context.Background(), options)
		requireErrorKind(t, err, KindInvalidOption)
	}
	empty, err := r.Discover(context.Background(), DiscoveryOptions{CandidateLimit: 5})
	if err != nil || empty.Incomplete || empty.Visited != 0 || len(empty.Checkpoints) != 0 {
		t.Fatalf("empty roots = %+v, %v", empty, err)
	}
}

func TestDiscoverBatchProcessCountDoesNotGrowWithCandidates(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	head, tip := f.git("rev-parse", "HEAD"), f.git("rev-parse", "HEAD")
	for i := 0; i < 24; i++ {
		tip = commitWithRawMessage(t, f, tip, checkpointBody(t, "main", head, fmt.Sprintf("checkpoint %d", i)))
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
	result, err := r.Discover(context.Background(), DiscoveryOptions{Roots: []string{tip, tip, head}, CandidateLimit: 30})
	if err != nil || result.Incomplete || result.Visited != 25 || len(result.Checkpoints) != 24 {
		t.Fatalf("batched chain = %+v, %v", result, err)
	}
	commands, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(commands)), "\n")
	if len(lines) > 6 {
		t.Fatalf("discovery spawned %d processes for 24 checkpoints:\n%s", len(lines), commands)
	}
	for _, line := range lines {
		if strings.Contains(line, " diff ") || strings.Contains(line, " check-ref-format ") {
			t.Fatalf("discovery spawned a per-candidate comparison/validation: %s", line)
		}
	}
}

func TestDiscoveryBranchValidationMatchesGit(t *testing.T) {
	f := newTestRepository(t, true)
	for _, branch := range []string{"main", "feature/topic", "@", "-dash", "żółw", "a./b", "name.locked", "", "/a", "a/", "a//b", ".hidden", "a/.hidden", "a.lock", "a/b.lock", "a..b", "a.", "a@{b", "a b", "a\tb", "a\nb", "a\x7fb", "a~b", "a^b", "a:b", "a?b", "a*b", "a[b", "a\\b"} {
		cmd := exec.Command(f.gitPath, "check-ref-format", checkpointRef(branch))
		cmd.Dir, cmd.Env = f.dir, f.env
		want := cmd.Run() == nil
		if got := validCheckpointBranch(branch); got != want {
			t.Errorf("branch %q: local validation=%v, Git=%v", branch, got, want)
		}
	}
}

func TestDiscoverSHA256(t *testing.T) {
	f := newTestRepository(t, false)
	if err := os.RemoveAll(filepath.Join(f.dir, ".git")); err != nil {
		t.Fatal(err)
	}
	f.git("init", "--object-format=sha256", "--initial-branch=main", "--template=")
	f.write("tracked.txt", "SHA-256 content\n")
	f.git("add", ".")
	f.git("commit", "-m", "SHA-256 base")
	r := openFixture(t, f)
	cp := captureFixture(t, r, CaptureOptions{Baseline: true})
	defer preserveReadFixture(t, f)()
	result, err := r.Discover(context.Background(), DiscoveryOptions{Roots: []string{cp.Commit, cp.HEAD}, CandidateLimit: 2})
	if err != nil || result.Incomplete || result.Visited != 2 || len(result.Checkpoints) != 1 || len(result.Checkpoints[0].Commit) != 64 || result.Checkpoints[0].Base != cp.HEAD {
		t.Fatalf("native SHA-256 discovery = %+v, %v", result, err)
	}
}

func TestDiscoverCancellationDuringRootBatch(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	head := f.git("rev-parse", "HEAD")
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
		result DiscoveryResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := r.Discover(ctx, DiscoveryOptions{Roots: []string{head}, CandidateLimit: 5})
		done <- outcome{result: result, err: err}
	}()
	waitForCaptureFilter(t, ready)
	cancel()
	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) || !got.result.Incomplete || len(got.result.Checkpoints) != 0 {
			t.Fatalf("active discovery cancellation = %+v, %v", got.result, got.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("discovery did not finish promptly after cancellation")
	}
}
