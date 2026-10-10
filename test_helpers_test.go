package wip

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testRepository runs real Git with isolated config and identity. Every repository,
// hook, worktree, and object created through it lives under the test's TempDir.
type testRepository struct {
	t       *testing.T
	dir     string
	gitPath string
	env     []string
}

func newTestRepository(t *testing.T, committed bool) *testRepository {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("real Git is required: %v", err)
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir, home := filepath.Join(base, "repository"), filepath.Join(base, "home")
	for _, path := range []string{dir, home} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	var env []string
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(name, "GIT_") || name == "HOME" || name == "XDG_CONFIG_HOME" || name == "LC_ALL" {
			continue
		}
		env = append(env, value)
	}
	env = append(env,
		"HOME="+home, "XDG_CONFIG_HOME="+home, "LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_NAME=Fixture Author", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
		"GIT_COMMITTER_NAME=Fixture Committer", "GIT_COMMITTER_EMAIL=fixture@example.invalid",
		"GIT_AUTHOR_DATE=2026-01-01T12:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T12:00:00Z",
	)
	// Library calls use the process environment; keep their ordinary config isolated too.
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	f := &testRepository{t: t, dir: dir, gitPath: gitPath, env: env}
	f.git("init", "--initial-branch=main", "--template=")
	f.git("config", "user.name", "Fixture Author")
	f.git("config", "user.email", "fixture@example.invalid")
	if committed {
		f.write("tracked.txt", "initial\n")
		f.git("add", "--", "tracked.txt")
		f.git("-c", "commit.gpgSign=false", "commit", "-m", "initial")
	}
	return f
}

func (f *testRepository) git(args ...string) string {
	f.t.Helper()
	cmd := exec.Command(f.gitPath, args...)
	cmd.Dir, cmd.Env = f.dir, f.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %q: %v\n%s", args, err, out)
	}
	return strings.TrimSuffix(string(out), "\n")
}

func (f *testRepository) write(path, contents string) {
	f.t.Helper()
	full := filepath.Join(f.dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(contents), 0600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *testRepository) read(path string) []byte {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.dir, filepath.FromSlash(path)))
	if err != nil {
		f.t.Fatal(err)
	}
	return data
}
