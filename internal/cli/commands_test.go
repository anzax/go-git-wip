package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anzax/go-git-wip"
)

type fixture struct {
	t   *testing.T
	dir string
	env []string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	base := t.TempDir()
	dir, home := filepath.Join(base, "repository"), filepath.Join(base, "home")
	for _, path := range []string{dir, home} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	var env []string
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(name, "GIT_") || name == "HOME" || name == "XDG_CONFIG_HOME" {
			continue
		}
		env = append(env, value)
	}
	env = append(env, "HOME="+home, "XDG_CONFIG_HOME="+home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_NAME=CLI Fixture", "GIT_AUTHOR_EMAIL=cli@example.invalid", "GIT_COMMITTER_NAME=CLI Fixture", "GIT_COMMITTER_EMAIL=cli@example.invalid")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	f := &fixture{t: t, dir: dir, env: env}
	f.git("init", "--initial-branch=main", "--template=")
	f.write("tracked.txt", []byte("initial\n"))
	f.git("add", ".")
	f.git("-c", "commit.gpgSign=false", "commit", "-m", "initial")
	return f
}

func (f *fixture) git(args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir, cmd.Env = f.dir, f.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %q: %v\n%s", args, err, out)
	}
	return strings.TrimSuffix(string(out), "\n")
}

func (f *fixture) write(path string, data []byte) {
	f.t.Helper()
	full := filepath.Join(f.dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0600); err != nil {
		f.t.Fatal(err)
	}
}

func invoke(f *fixture, args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), "test", append([]string{"-C", f.dir}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func decode(t *testing.T, data string, value any) {
	t.Helper()
	if err := json.Unmarshal([]byte(data), value); err != nil {
		t.Fatalf("invalid JSON %q: %v", data, err)
	}
}

func save(t *testing.T, f *fixture, args ...string) wip.CaptureResult {
	t.Helper()
	code, out, diagnostic := invoke(f, append([]string{"save", "--json"}, args...)...)
	if code != 0 || diagnostic != "" {
		t.Fatalf("save failed: %d, %q", code, diagnostic)
	}
	var result wip.CaptureResult
	decode(t, out, &result)
	return result
}

func TestCommandsJSONWorkflowAndHumanNoChange(t *testing.T) {
	f := newFixture(t)
	for _, command := range []string{"history", "refs", "reflog", "cleanup"} {
		code, out, diagnostic := invoke(f, command, "--json")
		if code != 0 || out != "[]\n" || diagnostic != "" {
			t.Fatalf("empty %s = %d, %q, %q", command, code, out, diagnostic)
		}
	}
	baseline := save(t, f, "--baseline", "-m", "review start")
	f.write("tracked.txt", []byte("changed\n"))
	changed := save(t, f)
	if !changed.Created || changed.Parent != baseline.Commit {
		t.Fatalf("capture result: %+v", changed)
	}
	code, out, diagnostic := invoke(f, "save")
	if code != 0 || out != "No changes\n" || diagnostic != "" {
		t.Fatalf("human nochange = %d, %q, %q", code, out, diagnostic)
	}
	code, out, diagnostic = invoke(f, "status", "--json")
	var status wip.Status
	decode(t, out, &status)
	if code != 0 || diagnostic != "" || !status.Active || status.Tip != changed.Commit {
		t.Fatalf("status = %+v, %d, %q", status, code, diagnostic)
	}
	code, out, diagnostic = invoke(f, "history", "--json", "--limit", "1", "--path", "tracked.txt")
	var history []wip.Checkpoint
	decode(t, out, &history)
	if code != 0 || diagnostic != "" || len(history) != 1 || history[0].Commit != changed.Commit {
		t.Fatalf("filtered history = %+v, %d, %q", history, code, diagnostic)
	}
	for _, args := range [][]string{{"show", changed.Commit, "--json", "--patch", "--path", "tracked.txt"}, {"diff", baseline.Commit, changed.Commit, "--json", "--patch"}} {
		code, out, diagnostic = invoke(f, args...)
		var comparison wip.Comparison
		decode(t, out, &comparison)
		if code != 0 || diagnostic != "" || comparison.From != baseline.Commit || comparison.To != changed.Commit || len(comparison.Files) != 1 || comparison.Patch == "" {
			t.Fatalf("comparison = %+v, %d, %q", comparison, code, diagnostic)
		}
	}
	code, out, diagnostic = invoke(f, "refs", "--json")
	var refs []wip.RefInfo
	decode(t, out, &refs)
	if code != 0 || diagnostic != "" || len(refs) != 1 || refs[0].Tip != changed.Commit {
		t.Fatalf("refs = %+v", refs)
	}
	code, out, diagnostic = invoke(f, "reflog", "--json", "--limit", "1")
	var reflog []wip.ReflogEntry
	decode(t, out, &reflog)
	if code != 0 || diagnostic != "" || len(reflog) != 1 || reflog[0].Commit != changed.Commit {
		t.Fatalf("reflog = %+v", reflog)
	}
}

func TestFileExactBinaryAndJSON(t *testing.T) {
	f := newFixture(t)
	content := []byte{0, 1, 255, '\n', 'x'}
	f.write("binary data.bin", content)
	checkpoint := save(t, f, "--untracked")
	code, out, diagnostic := invoke(f, "file", checkpoint.Commit, "binary data.bin")
	if code != 0 || diagnostic != "" || !bytes.Equal([]byte(out), content) {
		t.Fatalf("raw file = %d, %q, %q", code, out, diagnostic)
	}
	code, out, diagnostic = invoke(f, "file", checkpoint.Commit, "binary data.bin", "--json")
	var value struct {
		Revision string `json:"revision"`
		Path     string `json:"path"`
		Content  string `json:"content_base64"`
	}
	decode(t, out, &value)
	if code != 0 || diagnostic != "" || value.Revision != checkpoint.Commit || value.Path != "binary data.bin" || value.Content != base64.StdEncoding.EncodeToString(content) {
		t.Fatalf("JSON file = %+v", value)
	}
}

func TestDeletionApprovalConflictAndPartialCleanup(t *testing.T) {
	f := newFixture(t)
	baseline := save(t, f, "--baseline")
	f.write("tracked.txt", []byte("new\n"))
	changed := save(t, f)
	for _, args := range [][]string{{"delete", baseline.Ref, "--expect", changed.Commit}, {"delete", baseline.Ref, "--yes"}, {"cleanup", "--yes"}, {"cleanup", baseline.Ref + "=" + changed.Commit}} {
		code, out, _ := invoke(f, args...)
		if code != 2 || out != "" || f.git("rev-parse", baseline.Ref) != changed.Commit {
			t.Fatalf("missing deletion approval accepted: %q (%d, %q)", args, code, out)
		}
	}
	code, out, diagnostic := invoke(f, "delete", baseline.Ref, "--expect", baseline.Commit, "--yes", "--json")
	if code != 3 || out != "" || !strings.Contains(diagnostic, `"kind":"conflict"`) {
		t.Fatalf("conflict = %d, %q, %q", code, out, diagnostic)
	}
	f.git("switch", "-c", "orphan")
	orphan := save(t, f, "--baseline")
	f.git("switch", "main")
	f.git("branch", "-D", "orphan")
	code, out, diagnostic = invoke(f, "cleanup", "--json")
	var preview []wip.RefInfo
	decode(t, out, &preview)
	if code != 0 || diagnostic != "" || len(preview) != 1 || preview[0].Ref != orphan.Ref || !preview[0].Orphaned {
		t.Fatalf("cleanup preview = %+v", preview)
	}
	code, out, diagnostic = invoke(f, "cleanup", orphan.Ref+"="+orphan.Commit, changed.Ref+"="+changed.Commit, "--yes", "--json")
	var cleanup wip.CleanupResult
	decode(t, out, &cleanup)
	if code != 1 || len(cleanup.Deleted) != 1 || cleanup.Deleted[0] != orphan.Ref || len(cleanup.Failures) != 1 || !strings.Contains(diagnostic, `"kind":"failure"`) || strings.Contains(out, `"Err"`) {
		t.Fatalf("partial cleanup = %+v, %d, %q", cleanup, code, diagnostic)
	}
	code, out, diagnostic = invoke(f, "delete", changed.Ref, "--expect", changed.Commit, "--yes", "--json")
	var deleted wip.DeleteResult
	decode(t, out, &deleted)
	if code != 0 || diagnostic != "" || !deleted.Deleted {
		t.Fatalf("delete = %+v", deleted)
	}
	code, out, diagnostic = invoke(f, "delete", changed.Ref, "--expect", changed.Commit, "--yes", "--json")
	decode(t, out, &deleted)
	if code != 0 || diagnostic != "" || deleted.Deleted {
		t.Fatalf("missing delete = %+v", deleted)
	}
}

func TestUsageAndCancellationDiagnostics(t *testing.T) {
	f := newFixture(t)
	for _, args := range [][]string{{"unknown", "--json"}, {"--bad-flag", "--json"}, {"diff", "--json"}, {"history", "--limit", "-1", "--json"}, {"save", "--ignored", "--json"}} {
		code, out, diagnostic := invoke(f, args...)
		if code != 2 || out != "" || !strings.Contains(diagnostic, `"kind":"usage"`) || strings.Count(diagnostic, "\n") != 1 {
			t.Fatalf("usage %q = %d, %q, %q", args, code, out, diagnostic)
		}
	}
	code, out, diagnostic := invoke(f, "save", "-m", "--json", "--ignored")
	if code != 2 || out != "" || json.Valid([]byte(diagnostic)) || !strings.Contains(diagnostic, "--ignored requires --untracked") {
		t.Fatalf("message mistaken for a JSON flag: %d, %q, %q", code, out, diagnostic)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	code = Run(ctx, "test", []string{"-C", f.dir, "save", "--json"}, &stdout, &stderr)
	if code != 130 || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"kind":"cancelled"`) {
		t.Fatalf("cancellation = %d, %q, %q", code, stdout.String(), stderr.String())
	}
	f.git("switch", "--detach")
	code, out, diagnostic = invoke(f, "reflog", "--json")
	if code != 1 || out != "" || !strings.Contains(diagnostic, `"kind":"unsupported"`) {
		t.Fatalf("detached reflog = %d, %q, %q", code, out, diagnostic)
	}
}

type forbiddenStdin struct{}

func (forbiddenStdin) Read([]byte) (int, error) { panic("CLI read stdin") }

func TestDeletionNeverReadsStdin(t *testing.T) {
	cmd := NewCommand("test")
	cmd.SetArgs([]string{"delete", "refs/wip/main", "--expect", strings.Repeat("a", 40)})
	cmd.SetIn(forbiddenStdin{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("missing --yes should fail without reading stdin")
	}
}

func TestExecutableExitCodesAndSignals(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "go-git-wip")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/go-git-wip")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	f := newFixture(t)
	run := func(args ...string) (int, string, string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, append([]string{"-C", f.dir}, args...)...)
		cmd.Env, cmd.Stdin = f.env, strings.NewReader("yes\n")
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		code := 0
		if err != nil {
			if cmd.ProcessState == nil {
				t.Fatalf("CLI did not start: %v", err)
			}
			code = cmd.ProcessState.ExitCode()
		}
		return code, stdout.String(), stderr.String()
	}
	code, out, diagnostic := run("save", "--baseline", "--json")
	var baseline wip.CaptureResult
	decode(t, out, &baseline)
	if code != 0 || diagnostic != "" {
		t.Fatalf("executable save = %d, %q", code, diagnostic)
	}
	code, out, diagnostic = run("save")
	if code != 0 || out != "No changes\n" || diagnostic != "" {
		t.Fatalf("executable nochange = %d, %q, %q", code, out, diagnostic)
	}
	code, out, diagnostic = run("delete", baseline.Ref, "--expect", baseline.Commit, "--json")
	if code != 2 || out != "" || !strings.Contains(diagnostic, `"kind":"usage"`) {
		t.Fatalf("piped yes substituted for flag: %d, %q, %q", code, out, diagnostic)
	}
	f.write("tracked.txt", []byte("new checkpoint\n"))
	code, out, diagnostic = run("save", "--json")
	var changed wip.CaptureResult
	decode(t, out, &changed)
	if code != 0 || diagnostic != "" {
		t.Fatalf("executable changed save = %d, %q", code, diagnostic)
	}
	code, out, diagnostic = run("delete", baseline.Ref, "--expect", baseline.Commit, "--yes", "--json")
	if code != 3 || out != "" || !strings.Contains(diagnostic, `"kind":"conflict"`) {
		t.Fatalf("executable conflict = %d, %q, %q", code, out, diagnostic)
	}
	code, out, diagnostic = run("file", "does-not-exist", "tracked.txt", "--json")
	if code != 1 || out != "" || !strings.Contains(diagnostic, `"kind":"not_found"`) {
		t.Fatalf("executable operational error = %d, %q, %q", code, out, diagnostic)
	}

	// Signal the compiled program while real Git is staging through a clean filter.
	barrier := t.TempDir()
	ready, release, filter := filepath.Join(barrier, "ready"), filepath.Join(barrier, "release"), filepath.Join(barrier, "clean")
	script := "#!/bin/sh\nprintf ready > \"$1\"\nwhile test ! -e \"$2\"; do sleep 0.01; done\ncat\n"
	if err := os.WriteFile(filter, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	f.git("config", "filter.cli-test.clean", quote(filter)+" "+quote(ready)+" "+quote(release))
	f.git("config", "filter.cli-test.required", "true")
	f.write(".gitattributes", []byte("tracked.txt filter=cli-test\n"))
	f.write("tracked.txt", []byte("signal cancellation\n"))
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0600) })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-C", f.dir, "save", "--json")
	cmd.Env = f.env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr, cmd.WaitDelay = &stdout, &stderr, time.Second
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("executable did not reach clean filter")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil || cmd.ProcessState.ExitCode() != 130 || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"kind":"cancelled"`) {
		t.Fatalf("SIGINT = %v (%v), %q, %q", err, cmd.ProcessState, stdout.String(), stderr.String())
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if f.git("rev-parse", baseline.Ref) != changed.Commit {
		t.Fatal("interrupted capture advanced checkpoint ref")
	}
}
