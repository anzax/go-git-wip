# Library guide

For hook-driven workspace checkpoints and per-turn review endpoints, see
[agent-session integration](agent-sessions.md) and its runnable example.

Import `github.com/anzax/go-git-wip` as `wip`. The package uses standard-library
dependencies and local Git 2.30+ on Linux or macOS. Open a repository from an explicit
directory, including a subdirectory; calls never change the process working directory.

The [external consumer](../examples/workflow/main.go) is a complete, runnable workflow:

```sh
make example
# Or, from the repository root:
cd examples/workflow && go run .
```

Its separate Go module uses a local `replace` to test this checkout's public API.
The example creates and removes an isolated temporary repository, so running it does
not change an existing working tree. It creates a baseline, captures an edit and a
newly created file, verifies a no-change result, lists history, compares two full
commit IDs, and checks cancellation.

## Capture and review

Use the caller's context and an explicitly selected directory. A normal capture
returns `Created=false` and an empty `Commit` when its tree matches the current
checkpoint parent. That is a successful result, not an error.

```go
import (
    "context"

    wip "github.com/anzax/go-git-wip"
)

func save(ctx context.Context, directory string) (wip.CaptureResult, error) {
    repository, err := wip.Open(ctx, directory)
    if err != nil {
        return wip.CaptureResult{}, err
    }
    return repository.Capture(ctx, wip.CaptureOptions{Message: "review checkpoint"})
}
```

Use `CaptureOptions{Baseline: true}` to force a deliberate starting checkpoint.
`CaptureResult` returns the full commit ID, parent, checkpoint ref, and observed HEAD;
on no-change, `Parent`, `Ref`, and `HEAD` still describe the operation.
When `Created` is true, pass `Commit` to `Inspect` to compare it against its parent,
or use `Compare` with two explicit commit IDs selected from history.

Capture includes tracked edits/deletions and non-ignored untracked files by default,
including staged additions. Working content takes precedence over staged content
without changing the real index bytes or staging decisions. `TrackedOnly` excludes
untracked files; `IncludeIgnored` adds ignored files and conflicts with `TrackedOnly`.
These rules also apply to forced baselines. Selection never bypasses eligibility,
and included files are not checked for secrets.

`Paths` are literal files or directories relative to the directory passed to `Open`.
An empty list selects the whole repository. Each explicit path must match an
eligible file or directory descendant, including deleted tracked files. Unmatched
selections return `KindInvalidOption`, including mixtures of valid and unmatched
paths; ignored-only paths require `IncludeIgnored`, and untracked-only paths fail
with `TrackedOnly`. These errors leave the checkpoint ref and workspace unchanged.
Unchanged eligible files remain valid selections, but no-change depends on the
complete proposed snapshot matching its parent. Outside-root paths and paths through
symlink directories are rejected. Every snapshot starts from HEAD: capturing only B
after capturing A stores HEAD's version of A in the new snapshot. Partial captures
are independent snapshots rather than cumulative patches.

| Operation | Data returned |
| --- | --- |
| `Info`, `Status` | Current directory, branch and HEAD; checkpoint tip/base and whether its stack is active |
| `History` | Checkpoints newest first, ending before the normal base commit |
| `Refs` | Sorted owned refs, including refs whose normal branch has been deleted |
| `Reflog` | Retained entries newest first; ordinal selectors distinguish duplicate tips |
| `Inspect` | An owned checkpoint compared with its parent |
| `Compare` | Two explicit commit revisions resolved to full object IDs |
| `ReadFile` | Exact blob bytes; a stored symlink returns its target bytes |

`HistoryOptions.Revision` selects an older stack, including one discovered via a
reflog selector. The default selects the current branch's checkpoint tip even when
stale. Limits of zero mean unlimited; negative limits fail. History path filtering
keeps checkpoints whose parent comparison touches a selected path; the limit counts
matching checkpoints. Review filtering never changes the earlier capture's scope.

Comparisons expose A/M/D/T statuses, added/deleted line counts, and binary flags.
Rename detection is disabled, so a rename appears as deletion plus addition.
`IncludePatch` adds separate textual patch output. External diff and text conversion
are disabled. File reads preserve binary bytes and never restore working files.

## Ref lifecycle and deletion

Each branch owns `refs/wip/<branch>`, including branch names containing slashes.
While HEAD remains unchanged, captures extend one stack. A normal HEAD change makes
that stack stale; the next created checkpoint starts from the new HEAD. Earlier tips
can be found through retained reflogs while Git retains them. Missing history is an
empty result. Detached/unborn working trees support reads but have no active history;
capture requires an attached branch and an existing commit.

Deletion requires a full checkpoint ref and its observed full tip ID:

```go
deleted, err := repository.Delete(ctx, wip.DeleteOptions{
    Ref: observed.Ref, ExpectedTip: observed.Tip,
})
```

A changed tip yields `KindConflict`; an already-missing ref succeeds with
`Deleted=false`. Invalid metadata or unrelated/symbolic refs are never deleted.
`Cleanup` accepts explicit `[]wip.DeleteOptions` targets and checks that their normal
branches are still absent. Inspect both `Deleted` and `Failures`: cleanup can succeed
for some refs and fail for others. Cancellation or a repository-wide failure returns
the partial result and a top-level error. The library has no confirmation prompts;
applications own approval. No operation automatically prunes objects or runs Git GC.

Checkpoints are local and ephemeral, not a backup system. Ordinary reflog expiry and
garbage collection apply. Deleting a ref may also remove its reflog. Ownership
metadata guards accidental ref collisions; it is not a trust boundary against anyone
who can write the repository's objects and refs.

## Errors, cancellation, and limits

All operations accept `context.Context`; repositories can be reused concurrently.
Use `errors.Is(err, context.Canceled)` or `context.DeadlineExceeded` for cancellation,
and `errors.As(err, &detail)` with `var detail *wip.Error` for structured failures.
`detail.Kind` is the stable category, including `KindConflict`, `KindInvalidOption`,
`KindInvalidRef`, `KindNotFound`, and `KindUnsupported`. Git stderr is diagnostic
text, not an API to parse. Do not mutate option slices while a call is using them.

Capture uses a fixed noninteractive identity, `WIP checkpoint <wip@localhost>`, with
the current timestamp. This identity does not distinguish callers or agent sessions;
keep session identity in caller records and use a readable label in `Message` when
useful. Default messages are `Checkpoint` and `Baseline`. Version-2 commits place
the human message first and ownership metadata in a final footer. Git formats the
subject normally. Messages beginning `go-git-wip:` receive a visible `Checkpoint: `
prefix in raw Git display to disambiguate legacy storage; API `Checkpoint.Message`
returns the original bytes, including multiline text and trailing newlines. Existing
version-1 history remains supported, including mixed stacks, without rewriting refs
or commits. Signing and hooks are disabled; Git attributes and clean filters still
affect captured content. Sparse worktrees, unmerged indexes, and gitlinks/submodules are unsupported
for capture. Bare repositories and Windows are outside the supported scope.

Conditional updates detect competing checkpoint writers; they do not provide an
atomic snapshot while another process writes files or switches branches. A cancelled
or killed operation around publication can update a ref without returning success:
inspect `Status` or the ref before retrying. Harmless unreachable objects from failed
captures remain for normal Git maintenance.

Tests run on Linux/arm64 with Go 1.27.2 and Git 2.56.0. Linux and macOS are supported
targets; macOS and the stated minimum Git version have not been execution-tested.
See [architecture](architecture.md) for the storage protocol and transaction details,
and [CLI usage](cli.md) for the optional adapter's output and confirmation rules.
