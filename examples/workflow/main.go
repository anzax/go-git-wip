// This external consumer owns a disposable Git repository and uses only the
// public wip package. It never opens or modifies a caller's existing repository.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	wip "github.com/anzax/go-git-wip"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := run(ctx); err != nil {
		var detail *wip.Error
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			fmt.Fprintln(os.Stderr, "workflow cancelled:", err)
		case errors.As(err, &detail):
			fmt.Fprintf(os.Stderr, "workflow failed (%s): %v\n", detail.Kind, err)
		default:
			fmt.Fprintln(os.Stderr, "workflow failed:", err)
		}
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	directory, err := os.MkdirTemp("", "go-git-wip-example-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	// This standalone program owns its process environment. Isolating setup and
	// library calls prevents global Git config, hooks, identity, or GIT_DIR leakage.
	if err := isolateEnvironment(directory); err != nil {
		return err
	}
	root := filepath.Join(directory, "repository")
	if err := os.Mkdir(root, 0700); err != nil {
		return err
	}
	git := func(args ...string) (string, error) {
		command := exec.CommandContext(ctx, "git", args...)
		command.Dir = root
		out, err := command.CombinedOutput()
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out)), nil
	}
	for _, args := range [][]string{
		{"init", "--initial-branch=main", "--template="},
		{"config", "user.name", "Example Author"},
		{"config", "user.email", "example@localhost"},
	} {
		if _, err := git(args...); err != nil {
			return err
		}
	}
	file := filepath.Join(root, "note.txt")
	if err := os.WriteFile(file, []byte("initial\n"), 0600); err != nil {
		return err
	}
	for _, args := range [][]string{{"add", "--", "note.txt"}, {"commit", "-m", "initial"}} {
		if _, err := git(args...); err != nil {
			return err
		}
	}
	head, err := git("rev-parse", "HEAD")
	if err != nil {
		return err
	}

	repository, err := wip.Open(ctx, root)
	if err != nil {
		return err
	}
	baseline, err := repository.Capture(ctx, wip.CaptureOptions{Baseline: true, Message: "review start"})
	if err != nil {
		return err
	}
	if !baseline.Created || baseline.Commit == "" || baseline.Parent != head {
		return fmt.Errorf("unexpected baseline: %+v", baseline)
	}
	if err := os.WriteFile(file, []byte("initial\nedited\n"), 0600); err != nil {
		return err
	}
	// Newly created, non-ignored files are included without an extra option.
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("new file\n"), 0600); err != nil {
		return err
	}
	saved, err := repository.Capture(ctx, wip.CaptureOptions{Message: "edit note and add file"})
	if err != nil {
		return err
	}
	if !saved.Created || saved.Commit == "" || saved.Parent != baseline.Commit {
		return fmt.Errorf("unexpected edit capture: %+v", saved)
	}
	unchanged, err := repository.Capture(ctx, wip.CaptureOptions{})
	if err != nil {
		return err
	}
	if unchanged.Created || unchanged.Commit != "" || unchanged.Parent != saved.Commit {
		return fmt.Errorf("unexpected no-change result: %+v", unchanged)
	}
	history, err := repository.History(ctx, wip.HistoryOptions{Limit: 2})
	if err != nil {
		return err
	}
	if len(history) != 2 || history[0].Commit != saved.Commit || history[1].Commit != baseline.Commit ||
		history[0].Message != "edit note and add file" || history[1].Message != "review start" {
		return fmt.Errorf("unexpected history: %+v", history)
	}
	// Git subjects show human messages; API history omits ownership metadata.
	subject, err := git("log", "-1", "--format=%s", saved.Commit)
	if err != nil {
		return err
	}
	if subject != history[0].Message {
		return fmt.Errorf("unexpected Git subject: %q", subject)
	}
	// These ordinary full commit IDs also work with the caller's preferred reviewer.
	from, to := history[1].Commit, history[0].Commit
	comparison, err := repository.Compare(ctx, from, to, wip.CompareOptions{})
	if err != nil {
		return err
	}
	wantNew := wip.FileChange{Path: "new.txt", Status: "A", Added: 1}
	wantEdit := wip.FileChange{Path: "note.txt", Status: "M", Added: 1}
	if comparison.From != from || comparison.To != to || len(comparison.Files) != 2 || comparison.Files[0] != wantNew || comparison.Files[1] != wantEdit {
		return fmt.Errorf("unexpected comparison: %+v", comparison)
	}
	info, err := repository.Info(ctx)
	if err != nil {
		return err
	}
	if info.HEAD != head {
		return fmt.Errorf("capture changed normal HEAD: %s", info.HEAD)
	}
	// Cancellation remains recognizable through the library's structured error.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := repository.Status(cancelled); !errors.Is(err, context.Canceled) {
		return fmt.Errorf("expected cancellation, got %v", err)
	}
	fmt.Printf("Baseline: %s\nCapture:  %s\nNo changes: verified\n", from, to)
	for _, change := range comparison.Files {
		fmt.Printf("%s %s +%d -%d\n", change.Status, change.Path, change.Added, change.Deleted)
	}
	fmt.Println("Cancellation: verified")
	return nil
}

func isolateEnvironment(directory string) error {
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(name, "GIT_") {
			if err := os.Unsetenv(name); err != nil {
				return err
			}
		}
	}
	for name, value := range map[string]string{
		"HOME": directory, "XDG_CONFIG_HOME": directory, "LC_ALL": "C",
		"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null",
		"GIT_TERMINAL_PROMPT": "0",
	} {
		if err := os.Setenv(name, value); err != nil {
			return err
		}
	}
	return nil
}
