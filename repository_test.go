package wip

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func requireErrorKind(t *testing.T, err error, kind ErrorKind) {
	t.Helper()
	var failure *Error
	if !errors.As(err, &failure) || failure.Kind != kind {
		t.Fatalf("error = %v; want kind %s", err, kind)
	}
}

func TestOpenSubdirectoryPreservesWorkspace(t *testing.T) {
	f := newTestRepository(t, true)
	f.write("tracked.txt", "staged\n")
	f.git("add", "tracked.txt")
	f.write("tracked.txt", "working\n")
	sub := filepath.Join(f.dir, "nested", "space directory")
	if err := os.MkdirAll(sub, 0700); err != nil {
		t.Fatal(err)
	}
	index := f.read(".git/index")
	head := f.git("rev-parse", "HEAD")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	r, err := Open(context.Background(), sub)
	if err != nil {
		t.Fatal(err)
	}
	info, err := r.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Directory != sub || info.Root != f.dir || info.GitDir != filepath.Join(f.dir, ".git") || info.Branch != "main" || info.HEAD != head {
		t.Fatalf("unexpected repository info: %+v", info)
	}
	afterCwd, _ := os.Getwd()
	if afterCwd != cwd || !bytes.Equal(index, f.read(".git/index")) || string(f.read("tracked.txt")) != "working\n" || f.git("show", ":tracked.txt") != "staged" {
		t.Fatal("repository discovery changed cwd, real index, or working/staged content")
	}
}

func TestInfoRereadsBranchAndHEAD(t *testing.T) {
	f := newTestRepository(t, true)
	r, err := Open(context.Background(), f.dir)
	if err != nil {
		t.Fatal(err)
	}
	f.git("switch", "-c", "feature/nested")
	f.write("tracked.txt", "second\n")
	f.git("add", "tracked.txt")
	f.git("-c", "commit.gpgSign=false", "commit", "-m", "second")
	info, err := r.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Branch != "feature/nested" || info.HEAD != f.git("rev-parse", "HEAD") {
		t.Fatalf("cached branch or HEAD: %+v", info)
	}
	f.git("switch", "--detach", "HEAD")
	info, err = r.Info(context.Background())
	if err != nil || info.Branch != "" || info.HEAD == "" {
		t.Fatalf("detached info = %+v, error = %v", info, err)
	}
}

func TestOpenUnbornAndLinkedWorktree(t *testing.T) {
	f := newTestRepository(t, false)
	r, err := Open(context.Background(), f.dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := r.Info(context.Background())
	if err != nil || info.Branch != "main" || info.HEAD != "" {
		t.Fatalf("unborn info = %+v, error = %v", info, err)
	}
	f.write("tracked.txt", "initial\n")
	f.git("add", "tracked.txt")
	f.git("commit", "-m", "initial")
	linkedBase, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(linkedBase, "linked worktree")
	f.git("worktree", "add", "-b", "linked", linked)
	r, err = Open(context.Background(), linked)
	if err != nil {
		t.Fatal(err)
	}
	info, err = r.Info(context.Background())
	if err != nil || info.Root != linked || info.Branch != "linked" || info.GitDir == filepath.Join(linked, ".git") {
		t.Fatalf("linked info = %+v, error = %v", info, err)
	}
}

func TestOpenErrors(t *testing.T) {
	f := newTestRepository(t, false)
	t.Run("empty directory", func(t *testing.T) {
		_, err := Open(context.Background(), "")
		requireErrorKind(t, err, KindInvalidOption)
	})
	t.Run("not repository", func(t *testing.T) {
		_, err := Open(context.Background(), t.TempDir())
		requireErrorKind(t, err, KindNotRepository)
	})
	t.Run("bare", func(t *testing.T) {
		bare := filepath.Join(t.TempDir(), "bare.git")
		f.git("init", "--bare", "--template=", bare)
		_, err := Open(context.Background(), bare)
		requireErrorKind(t, err, KindUnsupported)
	})
	t.Run("missing Git", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := Open(context.Background(), f.dir)
		requireErrorKind(t, err, KindGitNotFound)
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := Open(ctx, f.dir)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v; want context.Canceled", err)
		}
	})
}

func TestExplicitRepositoryIgnoresInheritedRedirection(t *testing.T) {
	target, other := newTestRepository(t, true), newTestRepository(t, true)
	for name, value := range map[string]string{
		"GIT_DIR": filepath.Join(other.dir, ".git"), "GIT_WORK_TREE": other.dir,
		"GIT_COMMON_DIR": filepath.Join(other.dir, ".git"), "GIT_INDEX_FILE": filepath.Join(other.dir, "wrong-index"),
		"GIT_OBJECT_DIRECTORY":             filepath.Join(other.dir, "missing-objects"),
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": filepath.Join(other.dir, "missing-alternates"),
		"GIT_CEILING_DIRECTORIES":          target.dir, "GIT_NAMESPACE": "redirected",
		"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "core.bare", "GIT_CONFIG_VALUE_0": "true",
		"GIT_CONFIG_PARAMETERS": "'core.bare=true'",
	} {
		t.Setenv(name, value)
	}
	r, err := Open(context.Background(), target.dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := r.Info(context.Background())
	if err != nil || info.Root != target.dir || info.HEAD != target.git("rev-parse", "HEAD") {
		t.Fatalf("redirected operation: %+v, %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(other.dir, "wrong-index")); !os.IsNotExist(err) {
		t.Fatalf("inherited index was written: %v", err)
	}
}

func TestOpenGitTracingPolicy(t *testing.T) {
	for _, mode := range []struct {
		name     string
		open     func(context.Context, string) (*Repository, error)
		isolated bool
	}{
		{name: "Open", open: Open},
		{name: "zero options", open: func(ctx context.Context, dir string) (*Repository, error) {
			return OpenWithOptions(ctx, dir, OpenOptions{})
		}},
		{name: "isolated", isolated: true, open: func(ctx context.Context, dir string) (*Repository, error) {
			return OpenWithOptions(ctx, dir, OpenOptions{IgnoreInheritedGitEnvironment: true})
		}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			f := newTestRepository(t, true)
			traces := map[string]string{
				"GIT_TRACE":        filepath.Join(t.TempDir(), "trace"),
				"GIT_TRACE2_EVENT": filepath.Join(t.TempDir(), "trace2"),
			}
			for name, path := range traces {
				t.Setenv(name, path)
			}
			r, err := mode.open(context.Background(), f.dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, operation := range []string{"rev-parse", "symbolic-ref"} {
				if operation == "symbolic-ref" {
					if _, err := r.Info(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				for name, path := range traces {
					contents, err := os.ReadFile(path)
					if mode.isolated {
						if !os.IsNotExist(err) {
							t.Fatalf("%s wrote inherited %s destination: %v", operation, name, err)
						}
					} else if err != nil || !bytes.Contains(contents, []byte(operation)) {
						t.Fatalf("%s did not preserve inherited %s tracing: %q, %v", operation, name, contents, err)
					}
					if os.Getenv(name) != path {
						t.Fatalf("%s changed process environment", operation)
					}
				}
			}
		})
	}
}

func TestIsolatedRepositoryIgnoresGitConfigAndRoutingEnvironment(t *testing.T) {
	target, other := newTestRepository(t, true), newTestRepository(t, true)
	config := filepath.Join(t.TempDir(), "inherited-config")
	if err := os.WriteFile(config, []byte("[wip-test]\n\tvalue = inherited\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	// Confirm that ordinary opens retain this inherited configuration override.
	ordinary, err := Open(context.Background(), target.dir)
	if err != nil {
		t.Fatal(err)
	}
	value, err := ordinary.git(context.Background(), "config", "--get", "wip-test.value")
	if err != nil || string(value) != "inherited\n" {
		t.Fatalf("ordinary open did not retain inherited configuration: %q, %v", value, err)
	}
	for name, value := range map[string]string{
		"GIT_CONFIG_SYSTEM": config, "GIT_CONFIG_NOSYSTEM": "0",
		"GIT_DIR": filepath.Join(other.dir, ".git"), "GIT_WORK_TREE": other.dir,
		"GIT_COMMON_DIR": filepath.Join(other.dir, ".git"), "GIT_NAMESPACE": "redirected",
		"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "core.bare", "GIT_CONFIG_VALUE_0": "true",
	} {
		t.Setenv(name, value)
	}
	// HOME is retained, so ordinary configuration files still apply.
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".gitconfig"), []byte("[wip-test]\n\tvalue = preserved\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := OpenWithOptions(context.Background(), target.dir, OpenOptions{IgnoreInheritedGitEnvironment: true})
	if err != nil {
		t.Fatal(err)
	}
	info, err := r.Info(context.Background())
	if err != nil || info.Root != target.dir || info.GitDir != filepath.Join(target.dir, ".git") || info.HEAD != target.git("rev-parse", "HEAD") {
		t.Fatalf("redirected isolated operation: %+v, %v", info, err)
	}
	value, err = r.git(context.Background(), "config", "--get", "wip-test.value")
	if err != nil || string(value) != "preserved\n" {
		t.Fatalf("ordinary HOME configuration was not preserved: %q, %v", value, err)
	}
}

func TestIsolatedCapturePreservesIndexAndFixedIdentity(t *testing.T) {
	f := newTestRepository(t, true)
	f.write("tracked.txt", "staged\n")
	f.git("add", "tracked.txt")
	f.write("tracked.txt", "working\n")
	index, head := f.read(".git/index"), f.git("rev-parse", "HEAD")
	r, err := OpenWithOptions(context.Background(), f.dir, OpenOptions{IgnoreInheritedGitEnvironment: true})
	if err != nil {
		t.Fatal(err)
	}
	// Set these after discovery to verify that the policy persists for later calls.
	inheritedIndex := filepath.Join(t.TempDir(), "inherited-index")
	trace := filepath.Join(t.TempDir(), "trace")
	for name, value := range map[string]string{
		"GIT_INDEX_FILE": inheritedIndex, "GIT_TRACE": trace,
		"GIT_AUTHOR_NAME": "Inherited", "GIT_AUTHOR_EMAIL": "wrong@example.invalid",
		"GIT_COMMITTER_NAME": "Inherited", "GIT_COMMITTER_EMAIL": "wrong@example.invalid",
		"GIT_AUTHOR_DATE": "invalid inherited date", "GIT_COMMITTER_DATE": "invalid inherited date",
		"GIT_OPTIONAL_LOCKS": "1", "GIT_TERMINAL_PROMPT": "1",
	} {
		t.Setenv(name, value)
	}
	result := captureFixture(t, r, CaptureOptions{})
	if !result.Created || result.HEAD != head || result.Parent != head || f.git("show", result.Commit+":tracked.txt") != "working" {
		t.Fatalf("unexpected isolated capture: %+v", result)
	}
	if identity := f.git("show", "-s", "--format=%an|%ae|%cn|%ce", result.Commit); identity != "WIP checkpoint|wip@localhost|WIP checkpoint|wip@localhost" {
		t.Fatalf("wrong isolated checkpoint identity: %s", identity)
	}
	if !bytes.Equal(index, f.read(".git/index")) || f.git("show", ":tracked.txt") != "staged" || string(f.read("tracked.txt")) != "working\n" || f.git("rev-parse", "HEAD") != head {
		t.Fatal("isolated capture changed real index, staged content, working content, or HEAD")
	}
	for _, path := range []string{inheritedIndex, trace} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("isolated capture wrote inherited destination %s: %v", path, err)
		}
	}
}

func TestRunnerDisablesHooksAndSigning(t *testing.T) {
	f := newTestRepository(t, true)
	hooks := filepath.Join(t.TempDir(), "hooks")
	if err := os.MkdirAll(hooks, 0700); err != nil {
		t.Fatal(err)
	}
	// Failure makes any accidental hook invocation observable without shell output.
	if err := os.WriteFile(filepath.Join(hooks, "reference-transaction"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	f.git("config", "core.hooksPath", hooks)
	f.git("config", "commit.gpgSign", "true")
	f.git("config", "gpg.program", "/nonexistent/wip-test-signer")
	r, err := Open(context.Background(), f.dir)
	if err != nil {
		t.Fatal(err)
	}
	tree := f.git("rev-parse", "HEAD^{tree}")
	commit, err := r.gitEnv(context.Background(), []string{
		"GIT_AUTHOR_NAME=WIP checkpoint", "GIT_AUTHOR_EMAIL=wip@localhost",
		"GIT_COMMITTER_NAME=WIP checkpoint", "GIT_COMMITTER_EMAIL=wip@localhost",
	}, []byte("fixture checkpoint\n"), "commit-tree", tree, "-p", f.git("rev-parse", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.git(context.Background(), "update-ref", "refs/wip/main", string(bytes.TrimSpace(commit))); err != nil {
		t.Fatalf("hook ran despite suppression: %v", err)
	}
}

func TestRunnerCancellationDuringCommand(t *testing.T) {
	// A sleeping executable isolates process cancellation from Git workload timing.
	dir := t.TempDir()
	program := filepath.Join(dir, "blocking-git")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	r := &Repository{dir: dir, gitPath: program}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := r.git(ctx, "unused")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatalf("cancellation was not prompt or preserved: %v (%v)", err, time.Since(start))
	}
}
