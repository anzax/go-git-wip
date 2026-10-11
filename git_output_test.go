package wip

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func preserveReadFixture(t *testing.T, f *testRepository) func() {
	t.Helper()
	head, refs := f.git("rev-parse", "HEAD"), f.git("show-ref")
	index, working := f.read(".git/index"), f.read("tracked.txt")
	return func() {
		t.Helper()
		if f.git("rev-parse", "HEAD") != head || f.git("show-ref") != refs || !bytes.Equal(index, f.read(".git/index")) || !bytes.Equal(working, f.read("tracked.txt")) {
			t.Fatal("read operation changed HEAD, refs, index or working content")
		}
	}
}

func TestOutputCollectorBoundsWhileCollecting(t *testing.T) {
	cancelled := false
	b := outputCollector{limit: 10, stream: "stdout", cancel: func() { cancelled = true }}
	for _, p := range []string{"123", "4567", "890"} {
		if n, err := b.Write([]byte(p)); n != len(p) || err != nil {
			t.Fatalf("write %q = %d, %v", p, n, err)
		}
		if int64(cap(b.data)) > b.limit {
			t.Fatal("backing array capacity exceeded byte limit")
		}
	}
	if cancelled || string(b.data) != "1234567890" {
		t.Fatal("exact-limit output should succeed")
	}
	if n, err := b.Write([]byte("extra")); n != 0 || err == nil || !cancelled || len(b.data) != 10 {
		t.Fatalf("overflow = %d, %v; cancelled=%v, retained=%d", n, err, cancelled, len(b.data))
	}
	if n, err := b.Write([]byte("again")); n != 0 || err == nil || len(b.data) != 10 {
		t.Fatal("collector accepted data after overflow")
	}
}

func TestOutputLimitsReadBlobPatchAndCheckpointMessage(t *testing.T) {
	f := newTestRepository(t, true)
	r := openFixture(t, f)
	f.write("exact.txt", strings.Repeat("a", 512))
	f.write("large.txt", strings.Repeat("line\n", 2000))
	cp := captureFixture(t, r, CaptureOptions{})
	bounded, err := OpenWithOptions(context.Background(), f.dir, OpenOptions{MaxStdoutBytes: 512, MaxStderrBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	preserved := preserveReadFixture(t, f)
	exact, err := bounded.ReadFile(context.Background(), cp.Commit, "exact.txt")
	if err != nil || len(exact) != 512 {
		t.Fatalf("exact-limit blob = %d bytes, %v", len(exact), err)
	}
	out, err := bounded.ReadFile(context.Background(), cp.Commit, "large.txt")
	requireErrorKind(t, err, KindOutputLimit)
	if out != nil {
		t.Fatal("blob overflow returned truncated bytes")
	}
	comparison, err := bounded.Compare(context.Background(), cp.HEAD, cp.Commit, CompareOptions{IncludePatch: true})
	requireErrorKind(t, err, KindOutputLimit)
	if comparison.Patch != "" {
		t.Fatal("patch overflow returned truncated patch")
	}
	preserved()
	// Create a message larger than the cap using the default unlimited repository.
	large := captureFixture(t, r, CaptureOptions{Baseline: true, Message: strings.Repeat("message ", 2048)})
	// The intentional capture changes refs; establish the preservation point anew.
	preserved = preserveReadFixture(t, f)
	defer preserved()
	_, err = bounded.History(context.Background(), HistoryOptions{Revision: large.Commit, Limit: 1})
	requireErrorKind(t, err, KindOutputLimit)
	_, err = bounded.Refs(context.Background())
	requireErrorKind(t, err, KindOutputLimit)
	_, err = bounded.Inspect(context.Background(), large.Commit, CompareOptions{})
	requireErrorKind(t, err, KindOutputLimit)
	all, err := r.ReadFile(context.Background(), cp.Commit, "large.txt")
	if err != nil || len(all) != 10000 {
		t.Fatalf("zero-option default was changed: %d bytes, %v", len(all), err)
	}
}

func TestOutputLimitsStderrOptionsAndCancellation(t *testing.T) {
	f := newTestRepository(t, true)
	for _, options := range []OpenOptions{{MaxStdoutBytes: -1}, {MaxStderrBytes: -1}} {
		_, err := OpenWithOptions(context.Background(), f.dir, options)
		requireErrorKind(t, err, KindInvalidOption)
	}
	_, err := OpenWithOptions(context.Background(), f.dir, OpenOptions{MaxStdoutBytes: 1})
	requireErrorKind(t, err, KindOutputLimit)
	r, err := OpenWithOptions(context.Background(), f.dir, OpenOptions{MaxStderrBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	defer preserveReadFixture(t, f)()
	out, err := r.git(context.Background(), "not-a-git-command")
	requireErrorKind(t, err, KindOutputLimit)
	var detail *Error
	if !errors.As(err, &detail) || len(detail.Stderr) > 16 || !strings.Contains(detail.Message, "stderr") || out != nil {
		t.Fatalf("stderr overflow = %q, %v", out, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = r.git(ctx, "not-a-git-command")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancellation identity lost: %v", err)
	}
}

func TestConcurrentBoundedReads(t *testing.T) {
	f := newTestRepository(t, true)
	f.write("large.txt", strings.Repeat("a", 8192))
	cp := captureFixture(t, openFixture(t, f), CaptureOptions{})
	r, err := OpenWithOptions(context.Background(), f.dir, OpenOptions{MaxStdoutBytes: 512, MaxStderrBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	defer preserveReadFixture(t, f)()
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Go(func() {
			out, err := r.ReadFile(context.Background(), cp.Commit, "large.txt")
			var detail *Error
			if !errors.As(err, &detail) || detail.Kind != KindOutputLimit || out != nil {
				t.Errorf("concurrent read = %q, %v", out, err)
			}
		})
	}
	group.Wait()
}
