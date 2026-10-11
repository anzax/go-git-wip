//go:build linux || darwin

package wip

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The filter and its ordinary child both block until canceled. PID files allow
// cleanup even when this regression is run against the old direct-child runner.
func descendantCaptureFilter(t *testing.T, f *testRepository) (parentPID, childPID string) {
	t.Helper()
	dir := t.TempDir()
	parentPID, childPID = filepath.Join(dir, "parent"), filepath.Join(dir, "child")
	program := filepath.Join(dir, "clean")
	script := `#!/bin/sh
printf '%s\n' "$$" > "$1"
/bin/sh -c 'printf "%s\n" "$$" > "$1"; while :; do sleep 0.01; done' child "$2" &
wait
`
	if err := os.WriteFile(program, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	f.git("config", "filter.wip-descendant.clean", quote(program)+" "+quote(parentPID)+" "+quote(childPID))
	f.git("config", "filter.wip-descendant.required", "true")
	f.write(".gitattributes", "tracked.txt filter=wip-descendant\n")
	t.Cleanup(func() {
		for _, path := range []string{childPID, parentPID} {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err == nil && pid > 0 {
				process, err := os.FindProcess(pid)
				if err == nil {
					_ = process.Kill()
				}
			}
		}
	})
	return parentPID, childPID
}

func filterPID(t *testing.T, path string) int {
	t.Helper()
	waitForCaptureFilter(t, path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid filter PID %q: %v", data, err)
	}
	return pid
}

func processRunning(t *testing.T, pid int) bool {
	t.Helper()
	// A killed orphan can briefly remain a zombie until the host reaps it. That
	// is no longer executing; kill(pid, 0) alone cannot distinguish the states.
	output, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			t.Fatalf("read process state: %v", err)
		}
		return false
	}
	state := strings.TrimSpace(string(output))
	return state != "" && !strings.HasPrefix(state, "Z")
}

func TestCaptureCancellationStopsFilterDescendants(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "cancel"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			f := newTestRepository(t, true)
			r := openFixture(t, f)
			baseline := captureFixture(t, r, CaptureOptions{Baseline: true})
			f.write("tracked.txt", "staged content\n")
			f.git("add", "--", "tracked.txt")
			f.write("tracked.txt", "working content\n")
			parentFile, childFile := descendantCaptureFilter(t, f)
			index := f.read(".git/index")
			work := f.read("tracked.txt")
			attributes := f.read(".gitattributes")
			temporary := t.TempDir()
			t.Setenv("TMPDIR", temporary)

			// This sibling stays in the caller's process group. A cancellation
			// implementation must signal only the group created for Git.
			sibling := exec.Command("sleep", "30")
			if err := sibling.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sibling.Process.Kill(); _ = sibling.Wait() })

			ctx, cancel := context.WithCancel(context.Background())
			want := context.Canceled
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), time.Second)
				want = context.DeadlineExceeded
			}
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := r.Capture(ctx, CaptureOptions{}); done <- err }()
			parentPID, childPID := filterPID(t, parentFile), filterPID(t, childFile)
			if !deadline {
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, want) {
					t.Fatalf("capture error = %v, want %v", err, want)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("canceled capture did not finish promptly")
			}
			if processRunning(t, parentPID) || processRunning(t, childPID) {
				t.Fatal("clean-filter process or descendant survived cancellation")
			}
			if !processRunning(t, sibling.Process.Pid) {
				t.Fatal("cancellation stopped a process in the caller's group")
			}
			entries, err := os.ReadDir(temporary)
			if err != nil || len(entries) != 0 {
				t.Fatalf("capture left private index files: %v, %v", entries, err)
			}
			if !bytes.Equal(index, f.read(".git/index")) || !bytes.Equal(work, f.read("tracked.txt")) || !bytes.Equal(attributes, f.read(".gitattributes")) {
				t.Fatal("cancellation changed real index or working files")
			}
			if f.git("rev-parse", "HEAD") != baseline.HEAD || f.git("rev-parse", "refs/wip/main") != baseline.Commit {
				t.Fatal("cancellation changed HEAD or the checkpoint ref")
			}
		})
	}
}

func TestGitProcessGroupAndExitBeforeCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sleep", "30")
	configureGitProcess(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	group, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil || group != cmd.Process.Pid || group == syscall.Getpgrp() {
		t.Fatalf("Git group = %d, PID = %d, caller group = %d: %v", group, cmd.Process.Pid, syscall.Getpgrp(), err)
	}
	// Exit independently before context cancellation. No stale negative-PID
	// signal should be issued after the process has been reaped by Wait.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("expected killed command to fail")
	}
	cancel()
	if err := cmd.Cancel(); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("cancel after exit = %v, want os.ErrProcessDone", err)
	}
}

func TestGitCancellationAbsentGroup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sleep", "30")
	configureGitProcess(cmd)
	// Deliberately omit group creation to exercise ESRCH without fabricating a
	// PID that could name an unrelated group. The child remains in our group.
	cmd.SysProcAttr = nil
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	if err := cmd.Cancel(); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("cancel absent group = %v, want os.ErrProcessDone", err)
	}
	if !processRunning(t, cmd.Process.Pid) {
		t.Fatal("absent group cancellation killed the caller-group child")
	}
}

func TestRunnerWaitDelayAfterCommandExit(t *testing.T) {
	dir := t.TempDir()
	program, pidFile := filepath.Join(dir, "git"), filepath.Join(dir, "child")
	script := `#!/bin/sh
/bin/sh -c 'printf "%s\n" "$$" > "$1"; exec sleep 30' child "` + pidFile + `" &
while test ! -s "` + pidFile + `"; do sleep 0.01; done
exit 0
`
	if err := os.WriteFile(program, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err == nil && pid > 0 {
				process, err := os.FindProcess(pid)
				if err == nil {
					_ = process.Kill()
				}
			}
		}
	})
	r := &Repository{dir: dir, gitPath: program}
	start := time.Now()
	_, err := r.git(context.Background(), "unused")
	if !errors.Is(err, exec.ErrWaitDelay) || time.Since(start) > 2*time.Second {
		t.Fatalf("inherited output pipes were not bounded: %v (%v)", err, time.Since(start))
	}
	// Git exited before cancellation could be observed. The bounded pipe wait
	// does not promise descendant termination once its group leader is gone.
	if !processRunning(t, filterPID(t, pidFile)) {
		t.Fatal("expected the pipe-holding descendant to remain until cleanup")
	}
}

func TestOutputLimitStopsFilterDescendantsAndCleansPrivateIndex(t *testing.T) {
	f := newTestRepository(t, true)
	r, err := OpenWithOptions(context.Background(), f.dir, OpenOptions{MaxStdoutBytes: 4096, MaxStderrBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	baseline := captureFixture(t, r, CaptureOptions{Baseline: true})
	f.write("tracked.txt", "staged\n")
	f.git("add", "--", "tracked.txt")
	f.write("tracked.txt", "working\n")
	dir := t.TempDir()
	parentFile, childFile := filepath.Join(dir, "parent"), filepath.Join(dir, "child")
	program := filepath.Join(dir, "clean")
	script := `#!/bin/sh
printf '%s\n' "$$" > "$1"
/bin/sh -c 'printf "%s\n" "$$" > "$1"; exec sleep 30' child "$2" &
while test ! -s "$2"; do sleep 0.01; done
while :; do printf 'filter stderr exceeds its selected collection limit\n' >&2; done
`
	if err := os.WriteFile(program, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	f.git("config", "filter.wip-output.clean", quote(program)+" "+quote(parentFile)+" "+quote(childFile))
	f.git("config", "filter.wip-output.required", "true")
	f.write(".gitattributes", "tracked.txt filter=wip-output\n")
	t.Cleanup(func() {
		for _, path := range []string{childFile, parentFile} {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err == nil && pid > 0 {
				process, err := os.FindProcess(pid)
				if err == nil {
					_ = process.Kill()
				}
			}
		}
	})
	defer preserveReadFixture(t, f)()
	attributes := f.read(".gitattributes")
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = r.Capture(ctx, CaptureOptions{})
	requireErrorKind(t, err, KindOutputLimit)
	if processRunning(t, filterPID(t, parentFile)) || processRunning(t, filterPID(t, childFile)) {
		t.Fatal("output overflow left a filter or its ordinary descendant executing")
	}
	entries, err := os.ReadDir(temporary)
	if err != nil || len(entries) != 0 {
		t.Fatalf("output overflow left private-index files: %v, %v", entries, err)
	}
	if !bytes.Equal(attributes, f.read(".gitattributes")) || f.git("rev-parse", baseline.Ref) != baseline.Commit {
		t.Fatal("output overflow changed attributes or checkpoint ref")
	}
}
