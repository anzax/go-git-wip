package wip

import (
	"bytes"
	"context"
	"errors"
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
	cmd := exec.CommandContext(ctx, r.gitPath, gitArgs...)
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
	// A cancelled Git child may have spawned processes holding its output pipes.
	cmd.WaitDelay = 100 * time.Millisecond
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		op := "git"
		if len(args) > 0 {
			op = args[0]
		}
		return nil, &Error{Kind: KindGit, Op: op, Message: "Git command failed", Stderr: strings.TrimSpace(stderr.String()), Err: err}
	}
	return stdout.Bytes(), nil
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
