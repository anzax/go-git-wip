package wip

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repository targets one working tree and retains the invocation directory for
// resolving user-supplied paths. It holds no cached branch or HEAD state.
type Repository struct {
	dir                           string
	root                          string
	gitDir                        string
	gitPath                       string
	ignoreInheritedGitEnvironment bool
}

// Open discovers a non-bare working tree from an explicit directory, including a
// subdirectory. Detached and unborn working trees are valid for read operations.
func Open(ctx context.Context, directory string) (*Repository, error) {
	return OpenWithOptions(ctx, directory, OpenOptions{})
}

// OpenWithOptions discovers a working tree with an explicit Git environment policy.
// The selected policy applies during discovery and all subsequent operations.
func OpenWithOptions(ctx context.Context, directory string, options OpenOptions) (*Repository, error) {
	if err := ctx.Err(); err != nil {
		return nil, &Error{Kind: KindGit, Op: "open", Message: "operation cancelled", Err: err}
	}
	if directory == "" {
		return nil, &Error{Kind: KindInvalidOption, Op: "open", Message: "repository directory is required"}
	}
	dir, err := filepath.Abs(directory)
	if err != nil {
		return nil, &Error{Kind: KindInvalidOption, Op: "open", Message: "resolve repository directory", Err: err}
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, &Error{Kind: KindNotRepository, Op: "open", Message: "repository directory is unavailable", Err: err}
	}
	stat, err := os.Stat(dir)
	if err != nil || !stat.IsDir() {
		return nil, &Error{Kind: KindNotRepository, Op: "open", Message: "repository location must be a directory", Err: err}
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, &Error{Kind: KindGitNotFound, Op: "open", Message: "Git executable not found", Err: err}
	}
	gitPath, err = filepath.Abs(gitPath)
	if err != nil {
		return nil, &Error{Kind: KindGitNotFound, Op: "open", Message: "resolve Git executable", Err: err}
	}
	r := &Repository{dir: dir, gitPath: gitPath, ignoreInheritedGitEnvironment: options.IgnoreInheritedGitEnvironment}
	bare, err := r.git(ctx, "rev-parse", "--is-bare-repository")
	if err != nil {
		if gitExitCode(err, 128) {
			return nil, &Error{Kind: KindNotRepository, Op: "open", Message: "directory is not a Git repository", Err: err}
		}
		return nil, err
	}
	if strings.TrimSpace(string(bare)) == "true" {
		return nil, &Error{Kind: KindUnsupported, Op: "open", Message: "bare repositories are unsupported"}
	}
	root, err := r.git(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		if gitExitCode(err, 128) {
			return nil, &Error{Kind: KindNotRepository, Op: "open", Message: "directory is outside a Git working tree", Err: err}
		}
		return nil, err
	}
	r.root = strings.TrimSuffix(string(root), "\n")
	gitDir, err := r.git(ctx, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, err
	}
	r.gitDir = strings.TrimSuffix(string(gitDir), "\n")
	return r, nil
}

// Info rereads the current branch and HEAD without refreshing the real index.
func (r *Repository) Info(ctx context.Context) (RepositoryInfo, error) {
	if r == nil || r.root == "" || r.gitPath == "" {
		return RepositoryInfo{}, &Error{Kind: KindInvalidOption, Op: "info", Message: "repository must be opened first"}
	}
	info := RepositoryInfo{Directory: r.dir, Root: r.root, GitDir: r.gitDir}
	branch, err := r.git(ctx, "symbolic-ref", "--quiet", "HEAD")
	if err != nil && !gitExitCode(err, 1) {
		return RepositoryInfo{}, err
	}
	if err == nil {
		ref := strings.TrimSuffix(string(branch), "\n")
		if strings.HasPrefix(ref, "refs/heads/") {
			info.Branch = strings.TrimPrefix(ref, "refs/heads/")
		}
	}
	head, err := r.git(ctx, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err != nil {
		if info.Branch != "" && gitExitCode(err, 1) {
			return info, nil
		}
		return RepositoryInfo{}, err
	}
	info.HEAD = strings.TrimSpace(string(head))
	return info, nil
}
