# CLI contract

`go-git-wip` is a thin adapter over the public `wip` package. Git 2.30 or newer
and a supported Linux or macOS working tree are required for repository commands.
No arguments prints help and exits successfully. Help and version work outside a
repository. There are no interactive prompts, editor launches, or implicit writes.

## Commands

Global flags are `-C DIRECTORY` (default `.`) and `--json`. Paths are literal and
relative to that directory, including when it is below the repository root. Use
`--` before positional paths that begin with a dash. Revisions use ordinary Git
commit syntax; JSON object IDs returned by the library are always full IDs.

| Command | Behavior |
| --- | --- |
| `save [PATH...]` | Capture selected working content, or the whole repository when no paths are supplied |
| `status` | Current branch/HEAD, checkpoint ref/tip/base, and whether its stack is active |
| `history [REVISION]` | Checkpoints newest first; default is the current branch's tip, including a stale stack |
| `refs` | All owned checkpoint refs, including orphaned refs |
| `reflog [REF]` | Retained reflog records newest first; default is the current branch's checkpoint ref |
| `show REVISION` | An owned checkpoint's changes relative to its parent |
| `diff FROM TO` | Compare two explicit commit revisions |
| `file REVISION PATH` | Write exact historical file bytes to stdout |
| `delete REF --expect TIP --yes` | Conditionally delete one owned checkpoint ref |
| `cleanup` | Preview orphaned checkpoint refs without deleting anything |
| `cleanup REF=TIP [REF=TIP...] --yes` | Conditionally delete only the supplied, still-orphaned refs |
| `version`, `--version`, `-v` | Print the build version |

`save` accepts `--baseline`, `--tracked-only`, `--ignored`, and `--message/-m TEXT`.
Default saves include tracked and non-ignored untracked files. `--tracked-only`
excludes untracked files while retaining staged additions. `--ignored` also includes
ignored files and conflicts with `--tracked-only`. Explicit paths never override
eligibility. These rules also apply to forced baselines.
Baseline forces a checkpoint even when the tree is unchanged. Every snapshot starts
from HEAD, so a partial capture leaves unselected paths at HEAD content, not at the
previous checkpoint's content. Unchanged ordinary capture succeeds without a commit.

`history` and `reflog` accept `--limit N`; zero means unlimited and negative values
are usage errors. `history`, `show`, and `diff` accept repeatable `--path PATH` filters.
`show` and `diff` accept `--patch` to include a textual Git patch in addition to the
structured changed-file summary. Rename detection is disabled. History filtering
changes the displayed entries, not the scope of saved checkpoints.

A ref argument for `reflog`, `delete`, or `cleanup` is a complete `refs/wip/...`
name. `--expect` and cleanup TIP values are full observed commit IDs. Missing
checkpoint refs produce empty history/reflog or `deleted: false` as appropriate.
A missing file or invalid explicit revision is an error. Default history is empty
on detached or unborn HEAD. Default reflog on detached HEAD fails clearly because
there is no branch ref to select. Explicit comparisons, reads, history, reflogs,
and ref management remain available when their selected revisions exist.

## Deletion workflow

No command reads stdin for confirmation. Deletion always requires `--yes`; missing
confirmation or expected-tip input is a usage error, even with terminal stdin.
Neither piped input nor an affirmative answer can substitute for this flag.

First inspect `refs` or `cleanup`, then pass the exact ref and tip being approved:

```sh
go-git-wip -C ./project refs --json
go-git-wip -C ./project delete refs/wip/topic --expect FULL_TIP --yes

go-git-wip -C ./project cleanup --json
go-git-wip -C ./project cleanup refs/wip/deleted-topic=FULL_TIP --yes
```

`cleanup --yes` without explicit targets is a usage error. Supplying targets without
`--yes` is also a usage error. Preview returns the same `RefInfo` records as `refs`,
filtered to orphaned refs. Cleanup checks each target's ownership, observed tip, and
current orphan status through the library. Failures can coexist with successful
deletions; output reports both and the command exits nonzero. No object pruning or
Git garbage collection is performed.

## Output and exit codes

Human output is concise: saves report a full commit ID or `No changes`; status names
the branch, HEAD and active/stale/missing history; history and refs include full IDs;
comparison output lists change status, path and line counts or binary status.
The `file` command emits only the raw file bytes, without an added newline.
Diagnostics go to stderr, never mixed into normal stdout data.

`--json` emits one JSON value followed by a newline on stdout:

| Command | JSON shape |
| --- | --- |
| `save`, `status` | Public `CaptureResult`, `Status` object |
| `history`, `refs`, `reflog` | Array of public `Checkpoint`, `RefInfo`, `ReflogEntry` records |
| `show`, `diff` | Public `Comparison` object; requested `patch` included when nonempty |
| `file` | `{ "revision": "INPUT_REVISION", "path": "INPUT_PATH", "content_base64": "..." }` |
| `delete` | Public `DeleteResult` object |
| `cleanup` preview | Array of orphaned public `RefInfo` records |
| `cleanup` deletion | Public `CleanupResult` object |

Object keys follow public Go types' snake_case JSON tags. Top-level list results
use `[]` when empty. File revision/path echo the request; `content_base64` always
encodes the exact returned bytes. Timestamps use RFC 3339 as encoded by Go's
`time.Time`. Reflog selectors distinguish repeated records. Additional fields may
be added; consumers should ignore unknown keys. Help/version remain ordinary text
regardless of `--json`.

On failure with `--json`, stderr contains one JSON object shaped as
`{"error":{"kind":"CATEGORY","message":"DIAGNOSTIC"}}`. Categories are the
library's `ErrorKind` strings, `usage` for command parsing/confirmation, `cancelled`
for cancellation, and `failure` for other operational failures. Do not expose a
Go error object directly through JSON. Ordinary command failures do not emit a
success value. Partial cleanup is the exception: its result still goes to stdout,
with a failure diagnostic on stderr.

| Exit | Meaning |
| --- | --- |
| `0` | Success, including unchanged capture, empty lists, and already-missing deletion |
| `1` | Operational failure, unsupported repository state, or partial cleanup failure |
| `2` | Usage, invalid options, or missing explicit deletion confirmation |
| `3` | Conditional publication/deletion conflict |
| `130` | Context cancellation or interruption |

Partial cleanup uses exit 1 even if some individual failures are conflicts;
cancellation takes precedence and uses 130 while preserving any partial result.
The CLI maps errors to these categories, not directly to Git process exit codes.
SIGINT cancels the shared operation context. As with the library, cancellation
around publication may require inspecting the ref before retrying.

## Public consumer example

The runnable example imports only `github.com/anzax/go-git-wip` and standard-library
packages. It creates its own disposable Git working tree, opens it with a cancellable
context, creates a baseline, edits a file, creates another, captures both, and
demonstrates an unchanged capture. It lists history, chooses two returned full
commit IDs, and obtains their
structured comparison. Setup uses explicit Git argument arrays and an isolated
identity/configuration; it never mutates a caller's existing repository. Show error
handling with `errors.As` for `*wip.Error` and `errors.Is` for cancellation. Keep the
example small; it is an embedding workflow, not another command framework.
