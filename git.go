package wip

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func (r *Repository) git(ctx context.Context, args ...string) ([]byte, error) {
	return r.gitEnv(ctx, nil, nil, args...)
}

// gitEnv accepts only library-controlled environment overrides and argument arrays.
// Each call runs in the opened worktree; discovery runs in the invocation directory.
func (r *Repository) gitEnv(ctx context.Context, overrides []string, input []byte, args ...string) ([]byte, error) {
	dir := r.root
	if dir == "" {
		dir = r.dir
	}
	gitArgs := []string{
		"--no-pager", "--no-optional-locks", "--literal-pathspecs",
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "commit.gpgSign=false",
		"-c", "tag.gpgSign=false",
	}
	gitArgs = append(gitArgs, args...)
	commandCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, r.gitPath, gitArgs...)
	configureGitProcess(cmd)
	cmd.Dir = dir
	inherited := os.Environ()
	if r.ignoreInheritedGitEnvironment {
		filtered := make([]string, 0, len(inherited))
		for _, value := range inherited {
			name, _, _ := strings.Cut(value, "=")
			if !strings.HasPrefix(name, "GIT_") {
				filtered = append(filtered, value)
			}
		}
		inherited = filtered
	}
	cmd.Env = gitEnvironment(inherited, overrides)
	cmd.Stdin = bytes.NewReader(input)
	// Detached descendants or leftovers after Git exits can hold output pipes.
	cmd.WaitDelay = 100 * time.Millisecond
	stdout := outputCollector{limit: r.maxStdoutBytes, stream: "stdout", cancel: cancel}
	stderr := outputCollector{limit: r.maxStderrBytes, stream: "stderr", cancel: cancel}
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	op := "git"
	if len(args) > 0 {
		op = args[0]
	}
	if ctx.Err() == nil {
		for _, output := range []*outputCollector{&stdout, &stderr} {
			if output.err != nil {
				return nil, &Error{Kind: KindOutputLimit, Op: op, Message: output.err.Error(), Stderr: strings.TrimSpace(string(stderr.data)), Err: output.err}
			}
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return nil, &Error{Kind: KindGit, Op: op, Message: "Git command failed", Stderr: strings.TrimSpace(string(stderr.data)), Err: err}
	}
	return stdout.data, nil
}

// Each output pipe has one copying goroutine. Cancellation is concurrency-safe;
// fields are inspected only after Run joins those copying goroutines. Capacity
// grows within the limit so the retained backing array is bounded as well.
type outputCollector struct {
	data   []byte
	limit  int64
	stream string
	cancel context.CancelFunc
	err    error
}

func (b *outputCollector) Write(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	n := len(p)
	if b.limit > 0 && int64(n) > b.limit-int64(len(b.data)) {
		n = int(b.limit - int64(len(b.data)))
		b.err = fmt.Errorf("Git %s exceeds %d-byte output limit", b.stream, b.limit)
	}
	needed := len(b.data) + n
	if needed > cap(b.data) {
		capacity := needed
		if cap(b.data) <= int(^uint(0)>>1)/2 && cap(b.data)*2 > capacity {
			capacity = cap(b.data) * 2
		}
		if b.limit > 0 && int64(capacity) > b.limit {
			capacity = int(b.limit)
		}
		grown := make([]byte, len(b.data), capacity)
		copy(grown, b.data)
		b.data = grown
	}
	b.data = append(b.data, p[:n]...)
	if b.err != nil {
		b.cancel()
	}
	return n, b.err
}

func outputLimitError(err error) bool {
	var detail *Error
	return errors.As(err, &detail) && detail.Kind == KindOutputLimit
}

func gitEnvironment(inherited, overrides []string) []string {
	fixed := []string{"LC_ALL=C", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_NO_REPLACE_OBJECTS=1"}
	allOverrides := append(fixed, overrides...)
	names := make(map[string]bool, len(allOverrides))
	for _, value := range allOverrides {
		name, _, _ := strings.Cut(value, "=")
		names[name] = true
	}
	env := make([]string, 0, len(inherited)+len(allOverrides))
	for _, value := range inherited {
		name, _, _ := strings.Cut(value, "=")
		if names[name] || unsafeGitEnvironment(name) {
			continue
		}
		env = append(env, value)
	}
	return append(env, allOverrides...)
}

func unsafeGitEnvironment(name string) bool {
	switch name {
	case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE",
		"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM",
		"GIT_PREFIX", "GIT_NAMESPACE", "GIT_SHALLOW_FILE", "GIT_GRAFT_FILE",
		"GIT_REPLACE_REF_BASE", "GIT_CONFIG", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS",
		"GIT_LITERAL_PATHSPECS", "GIT_GLOB_PATHSPECS", "GIT_NOGLOB_PATHSPECS",
		"GIT_ICASE_PATHSPECS", "GIT_EXTERNAL_DIFF", "GIT_DIFF_OPTS":
		return true
	}
	return strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_")
}

func gitExitCode(err error, code int) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == code
}
