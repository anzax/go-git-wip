# Architecture

Public package `wip` implements native Git checkpoints. The module is
`github.com/anzax/go-git-wip`; the library uses the standard library and local Git
2.30 or newer. Linux and macOS are supported targets; actual validation platforms
are recorded in task verification. The CLI owns Cobra, presentation, confirmation,
and exit codes. There is no database or alternative Git backend.

## Public contract

`Open(ctx, directory)` returns a `*Repository` anchored to an explicit invocation
directory and its working tree. It accepts subdirectories and attached, detached,
or unborn working trees; bare repositories are unsupported. Capture requires an
attached local branch with a commit. Repository state is read anew for each operation.
All operations accept a context, and all returned object IDs are full IDs.

`OpenWithOptions(ctx, directory, OpenOptions)` adds an opt-in environment policy; `Open` delegates with zero options. `IgnoreInheritedGitEnvironment: true` copies an immutable policy into the repository and strips every inherited `GIT_*` variable during discovery and every subsequent Git command. Concurrent operations reuse that policy while reading the current environment independently. Non-Git environment variables are preserved, and library-controlled safety, identity, and index overrides are supplied after filtering. Ordinary Git configuration files remain active; this option isolates inherited Git environment, not repository configuration or clean filters.

| Method                                   | Result and purpose                                                        |
| ---------------------------------------- | ------------------------------------------------------------------------- |
| `Info(ctx)`                              | `RepositoryInfo`: invocation directory, root, Git directory, branch, HEAD |
| `Capture(ctx, CaptureOptions)`           | `CaptureResult`: publication or successful no-change                      |
| `Status(ctx)`                            | `Status`: current branch, checkpoint tip/base, active-history flag        |
| `History(ctx, HistoryOptions)`           | `[]Checkpoint`: newest first along one owned stack                        |
| `Refs(ctx)`                              | `[]RefInfo`: lexicographically sorted owned refs, including orphans       |
| `Reflog(ctx, ref, limit)`                | `[]ReflogEntry`: retained records, newest first                           |
| `Inspect(ctx, revision, CompareOptions)` | `Comparison`: owned checkpoint versus its parent                          |
| `Compare(ctx, from, to, CompareOptions)` | `Comparison`: arbitrary explicit commit revisions                         |
| `ReadFile(ctx, revision, path)`          | `[]byte`: exact blob contents, including binary files                     |
| `Delete(ctx, DeleteOptions)`             | `DeleteResult`: conditional deletion of one owned ref                     |
| `Cleanup(ctx, []DeleteOptions)`          | `CleanupResult`: explicit orphan targets and partial failures             |

Each method also returns `error`. `Error.Kind` is the stable classification;
`errors.As` exposes its details and `errors.Is` retains context cancellation through
`Unwrap`. Git stderr is diagnostic material, never a parsing contract. Repository
values can be reused concurrently, but mutation conflicts are reported rather than
serialized by a process-local lock. Callers must not mutate option slices during use.

History defaults to the current branch's tip even if stale; its checkpoint metadata
identifies the old base. An explicit revision selects an older stack, including a
reflog-discovered tip. Traversal stops before the normal base commit. Zero limits
mean unlimited; negative limits are invalid. A missing current checkpoint ref gives
empty history/reflog data and an inactive status, not an error. Detached/unborn
status has no active history; branch-independent reads and ref listing still work.
History filters retain checkpoints whose parent comparison touches a selected path;
the limit applies to matching checkpoints. Reflog records retain duplicates and
identify them with their ordinal selectors. Git reflog expiry still applies.

Comparisons disable rename detection, external diff and text conversion. Structured
changes expose A/M/D/T, path, added/deleted lines and binary status; renames are a
separate deletion and addition. Optional textual patches are separate from those
records. Revision inputs resolve to commits before use; option-like revision strings
must never become Git command options. Paths are literal, not Git pathspec programs.
A historical file read resolves an exact repository-relative blob path and does not
follow stored symlinks; a symlink yields its stored target bytes.

## Capture transaction

Every capture starts from the current **HEAD tree**, including partial captures.
Unselected paths therefore contain HEAD content. Partial checkpoints are independent
snapshots, not cumulative patches: capturing A then only B records HEAD's version of
A in the second checkpoint. This keeps omitted paths predictable.

Tracked eligibility is the union of HEAD paths and real-index paths: staged additions
count as tracked, staged deletion does not hide a still-existing working file, and
working-file content always wins over staged content. Missing eligible paths are
removed. Non-ignored untracked files are included by default; `TrackedOnly` excludes
them. Ignored untracked files require `IncludeIgnored`, which conflicts with
`TrackedOnly`. These rules also apply to forced baselines. Explicit path selection
never bypasses these policies.
An empty path list selects the whole repository, even if eligibility is empty.
Validate each explicit selection against the complete eligible set before filtering
or allocating the private index. An unmatched selection returns `KindInvalidOption`
and identifies the normalized repository-relative selection; mixed valid/unmatched
selections also fail. Selected directories match their contents, including deleted
tracked descendants. Unchanged eligible files remain valid: no-change still compares
the complete HEAD-based proposed snapshot with the chosen parent. Outside-root paths
and paths through symlink directories are rejected; a tracked symlink itself is a normal Git entry.
Sparse working trees, unmerged indexes, and gitlinks/submodules are unsupported for
capture until their semantics receive dedicated tests.

1. Read the local branch, full HEAD ID, and direct checkpoint ref. Reject symbolic
   checkpoint refs, invalid names, and unrelated existing commit targets.
2. The parent is the existing owned tip if its recorded base equals HEAD; otherwise
   the parent is HEAD. A changed HEAD starts a new stack at publication, retaining
   the old tip only through the reflog under normal Git retention rules.
3. Enumerate eligible paths with NUL-delimited Git plumbing, validate every explicit
   selection, then intersect literal selection. Create a private temporary index,
   read HEAD's tree into it, and apply working content/deletions only for those paths.
   Never write or refresh the real index. Do not construct shell commands.
4. Write the resulting tree. Compare it against the chosen parent's tree. Equal
   trees return `Created=false`, empty `Commit`, and the observed parent/ref/HEAD,
   without changing the ref. `Baseline=true` forces a commit even when equal.
5. `commit-tree` creates a single-parent commit. Publish with an expected old tip
   (or expected absence), `--no-deref`, and a reflog. Verify the observed work-branch
   OID in the publication transaction where supported. A competing tip update
   returns `KindConflict` and is never overwritten.
6. Remove the private index and its lock/temp directory on every return path.

Identity is deliberately deterministic: author and committer are `WIP checkpoint
<wip@localhost>`, with Git's current timestamp. No user identity configuration is
required. The default message is `Checkpoint`, or `Baseline` when requested.
Commit signing and hooks, including reference-transaction hooks, are disabled for
library commands. Git attributes and clean filters still affect captured content
like ordinary Git staging; callers control repository filter configuration.

Before success, check that HEAD still denotes the observed branch and commit.
Conditional ref publication protects concurrent checkpoint writers and observed
branch OIDs, but no portable Git operation atomically locks symbolic HEAD, index,
and every working file. External branch switches or working writes during capture
are unsupported races; this library does not promise an atomic filesystem snapshot.
A cancellation or process termination around publication can leave the ref updated
without a success response; inspect the ref before retrying. Unreachable objects
from abandoned captures are harmless and left for Git's ordinary maintenance.

## Owned storage and deletion

Refs use `refs/wip/<short-local-branch>`, including slash-containing branch names.
New checkpoints use version 2: a human message followed by a blank-line separator
and one final metadata line. With the default ordinary message, the exact body is:

```text
Checkpoint

go-git-wip:2 {"branch":"main","base":"FULL_OBJECT_ID","message_bytes":10}
```

The byte layout is `DISPLAY_MESSAGE + "\n\ngo-git-wip:2 " + METADATA_JSON + "\n"`.
`message_bytes` counts the original message's bytes after the empty-message default
(`Checkpoint` or `Baseline`). The metadata has exactly `branch`, `base`, and
`message_bytes`; the last is a nonnegative integer. Reject missing, duplicate,
unknown, or incorrectly typed fields, invalid lengths, and malformed framing.
Key order and insignificant JSON whitespace may vary, but the metadata occupies
one line followed by exactly one final newline. Caller whitespace and trailing
newlines are preserved, and NUL is invalid.

Raw bodies beginning `go-git-wip:` are reserved for version-1 dispatch. To avoid
ambiguity, version-2 display messages beginning that namespace receive the visible
prefix `Checkpoint: `; the stored length still describes the original message.
The API removes only that canonical display prefix when length and namespace match.
Ordinary human messages appear directly as Git subjects under Git's normal subject
formatting. Fixed Git author identity does not identify an agent or session; callers
keep that identity in their own records and may put a readable label in the message.

Readers support version 1 and 2 in either order within one stack. Version 1 has
`go-git-wip:1 {"branch":"...","base":"..."}` on its first line, a blank line,
and caller text; decoding removes the old writer's one final newline. A raw body
beginning the reserved namespace must parse as version 1, without footer fallback,
so footer-looking version-2 text in its caller message remains ordinary text.
Otherwise, only the final footer line may define a version-2 envelope, with exact
separator, final newline, and message-byte framing. Unknown versions fail without
trying another format. Existing commits and refs need no migration or rewriting.

Decode the envelope strictly, require the matching branch, a valid full base commit
ID, and exactly one parent. The tip's parent must be its base or another owned
checkpoint with the same branch/base. Capture only needs this bounded local
validation; history validates every traversed entry and its boundary. The API
strips the envelope from `Checkpoint.Message`. Unknown versions, malformed metadata,
wrong branch/base, noncommit refs, and symbolic refs are errors, never overwritten
or deleted. This protocol detects accidental collisions; it is not a cryptographic
ownership or trust boundary against someone who can write Git objects and refs.
Ref listing fails explicitly on an invalid entry rather than silently hiding it.

Deletion requires an explicit complete checkpoint ref and observed `ExpectedTip`.
Validate ownership before conditional removal. A missing ref is a successful
`Deleted=false`, while a changed tip is `KindConflict`. Normal branches, HEAD,
index, and working files remain untouched. Ref deletion can remove its reflog.
Cleanup accepts only explicitly supplied targets that are still orphaned (their
normal branches do not exist); it never selects all refs implicitly. Each failed
entry appears in `Failures`, successful removals in `Deleted`. Individual target
failures do not produce a top-level error; cancellation or repository-wide failure
returns both the partial result and an error. This is not an atomic batch, and a
normal branch can be recreated concurrently. No automatic garbage collection occurs.

## Git execution boundary

Use `exec.CommandContext`, argument arrays and explicit command directories.
Preserve normal Git configuration while stripping inherited variables capable of
redirecting the repository, index, namespace, object storage, or injected command
configuration. Supply operation-specific index and identity overrides explicitly.
With `IgnoreInheritedGitEnvironment`, strip all inherited `GIT_*` variables before applying those library-controlled overrides; default opens retain the existing selective filtering policy.
Disable optional index writes, fsmonitor, signing, hooks, external diff, textconv,
and interactive prompting where applicable. Capture stderr privately. NUL-delimited
output protects filenames containing spaces, tabs, and newlines. Do not assume SHA-1
length: derive and validate full object IDs for the repository's object format.

On Linux/macOS, start each Git command with `Setpgid` and a new process group identified by its PID. Override `CommandContext.Cancel` to SIGKILL only that group while its Git leader remains active, including ordinary filter descendants; retain a 100 ms `WaitDelay` for inherited output pipes. Check the command process before signaling and treat an absent group as `os.ErrProcessDone` so an exit racing cancellation follows the standard runner contract. If the leader has already exited before cancellation is observed, descendants can survive the bounded wait: do not signal a stale group after its leader has been reaped. Deliberately detached descendants are also outside the guarantee; no process-tree enumeration or host-wide signaling is performed.

Keep the implementation concrete: one repository type and private execution helpers,
not a backend interface or command framework. Tests use real temporary repositories
with isolated Git identity/configuration and assert exact object/tree/index behavior.
The embedding example owns its workflow and cancellation; review tools, daemons,
watchers, restore operations, hook installers, and editor integration stay outside
this library.

Git's documented [commit-tree](https://git-scm.com/docs/git-commit-tree) and
[update-ref](https://git-scm.com/docs/git-update-ref) contracts define object creation,
conditional publication, and reflog behavior. Implementation tests cross-check
stored trees, parents, refs, and workspace preservation with Git itself.
