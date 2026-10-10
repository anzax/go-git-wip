package wip

import "time"

// ErrorKind identifies a stable failure category independently of Git's wording.
type ErrorKind string

const (
	KindGitNotFound   ErrorKind = "git_not_found"
	KindNotRepository ErrorKind = "not_repository"
	KindUnsupported   ErrorKind = "unsupported"
	KindInvalidOption ErrorKind = "invalid_option"
	KindInvalidRef    ErrorKind = "invalid_ref"
	KindNotFound      ErrorKind = "not_found"
	KindConflict      ErrorKind = "conflict"
	KindGit           ErrorKind = "git"
)

// Error describes an operation failure. Err retains cancellation and process errors.
// Callers may use errors.As for *Error and errors.Is for context cancellation.
type Error struct {
	Kind    ErrorKind
	Op      string
	Message string
	Stderr  string
	Err     error
}

// RepositoryInfo describes the explicitly opened working tree. Branch is the short
// local branch name, empty when detached; HEAD is empty for an unborn branch.
type RepositoryInfo struct {
	Directory string `json:"directory"`
	Root      string `json:"root"`
	GitDir    string `json:"git_dir"`
	Branch    string `json:"branch"`
	HEAD      string `json:"head"`
}

// CaptureOptions selects working files over a fresh HEAD tree. Paths are literal
// paths relative to the directory passed to Open; an empty list selects all paths.
// Each explicit path must match an eligible file or directory descendant, including
// deleted tracked files; an unmatched selection returns KindInvalidOption.
// Tracked and non-ignored untracked files are included by default. TrackedOnly
// excludes untracked files and conflicts with IncludeIgnored. Baseline forces
// creation even unchanged.
type CaptureOptions struct {
	Paths          []string `json:"paths,omitempty"`
	TrackedOnly    bool     `json:"tracked_only"`
	IncludeIgnored bool     `json:"include_ignored"`
	Baseline       bool     `json:"baseline"`
	Message        string   `json:"message,omitempty"`
}

// CaptureResult describes a published checkpoint or a successful no-change result.
// Commit is empty when Created is false; Parent still identifies the comparison base.
type CaptureResult struct {
	Created bool   `json:"created"`
	Commit  string `json:"commit"`
	Parent  string `json:"parent"`
	Ref     string `json:"ref"`
	HEAD    string `json:"head"`
}

// Status reports whether the current branch's owned checkpoint stack matches HEAD.
type Status struct {
	Repository RepositoryInfo `json:"repository"`
	Ref        string         `json:"ref"`
	Tip        string         `json:"tip"`
	Base       string         `json:"base"`
	Active     bool           `json:"active"`
}

// Checkpoint is a validated, single-parent checkpoint. Message excludes storage metadata.
type Checkpoint struct {
	Commit  string    `json:"commit"`
	Parent  string    `json:"parent"`
	Base    string    `json:"base"`
	Branch  string    `json:"branch"`
	Message string    `json:"message"`
	Time    time.Time `json:"time"`
}

// HistoryOptions chooses a stack tip (default: current branch's checkpoint ref).
// Limit zero means unlimited; negative limits are invalid. Paths filter checkpoints
// by changes against their own parent and are relative to the invocation directory.
type HistoryOptions struct {
	Revision string   `json:"revision,omitempty"`
	Limit    int      `json:"limit"`
	Paths    []string `json:"paths,omitempty"`
}

// RefInfo describes a validated checkpoint ref, including deleted normal branches.
type RefInfo struct {
	Ref      string `json:"ref"`
	Branch   string `json:"branch"`
	Tip      string `json:"tip"`
	Base     string `json:"base"`
	Orphaned bool   `json:"orphaned"`
}

// ReflogEntry identifies an individual retained reflog record. Repeated commits are
// retained, distinguished by Selector (for example refs/wip/main@{1}).
type ReflogEntry struct {
	Selector string `json:"selector"`
	Commit   string `json:"commit"`
	Message  string `json:"message"`
}

// CompareOptions filters an explicit pair of commit revisions. IncludePatch opts
// into textual Git diff output; structured files are always returned.
type CompareOptions struct {
	Paths        []string `json:"paths,omitempty"`
	IncludePatch bool     `json:"include_patch"`
}

// FileChange uses Git status letters A, M, D, or T. Rename detection is disabled;
// renames appear as a deletion plus an addition. Binary changes have no line counts.
type FileChange struct {
	Path    string `json:"path"`
	Status  string `json:"status"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	Binary  bool   `json:"binary"`
}

// Comparison contains resolved full commit IDs and their tree changes.
type Comparison struct {
	From  string       `json:"from"`
	To    string       `json:"to"`
	Files []FileChange `json:"files"`
	Patch string       `json:"patch,omitempty"`
}

// DeleteOptions requires an observed full object ID to avoid removing a newer tip.
// Ref must be a complete refs/wip/ name; a missing ref returns Deleted false.
type DeleteOptions struct {
	Ref         string `json:"ref"`
	ExpectedTip string `json:"expected_tip"`
}

// DeleteResult reports the outcome of one conditional ref deletion.
type DeleteResult struct {
	Ref     string `json:"ref"`
	Deleted bool   `json:"deleted"`
}

// CleanupFailure records a target that could not be deleted. Err is available to
// Go callers; Message supplies the JSON representation.
type CleanupFailure struct {
	Ref     string `json:"ref"`
	Message string `json:"message"`
	Err     error  `json:"-"`
}

// CleanupResult records partial outcomes; cleanup is not an atomic batch.
type CleanupResult struct {
	Deleted  []string         `json:"deleted"`
	Failures []CleanupFailure `json:"failures"`
}
