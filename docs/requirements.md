# Checkpoint requirements

Status: implementation contract. The settled public API and storage policies are
defined in [architecture](architecture.md); task records track implemented behavior
and validation separately.

## Purpose and boundaries

Provide an MIT-licensed Go library that applications can embed to capture temporary
working-tree checkpoints, inspect changes, and manage local checkpoint history.
Use the local Git executable and native commits, trees, refs, and reflogs. A thin
CLI exposes the same operations for people and scripts.

The package has its own behavior and API. Compatibility with another tool is not
an acceptance criterion. No separate database, service, or Git implementation is
needed. Applications own task tracking, agent hooks, and review orchestration.

## Repository and library contract

- Open a repository from an explicit directory, including a subdirectory. Return
  its resolved working-tree location and branch/ref information without changing
  the process working directory.
- Capture initially requires a non-bare repository, an existing commit, and an
  attached local branch. Return clear errors for missing Git, non-repositories,
  unsupported states, invalid options, and failed Git commands.
- Scope checkpoints to branches, initially using `refs/wip/<branch>`. Handle branch
  names containing slashes. Validate ref names and existing ref targets before writes;
  do not silently replace invalid or unrelated history.
- All potentially long-running operations accept context. Return typed results and
  errors so callers need not parse terminal output or Git's human error wording.
- On Linux/macOS, context cancellation and deadlines while Git remains active stop its process group, including ordinary filter descendants, while preserving context error identity and bounded output-pipe waits. Do not signal the caller's group or unrelated commands. Descendants that detach into another process group, or survive a leader that exited before cancellation was observed, are outside the termination guarantee.
- Keep library calls free of prompts, stdout/stderr writes, process exits, and hidden
  configuration changes. Use explicit API options for initial configuration.
- Preserve the user's Git environment where appropriate while preventing inherited
  repository/index variables from redirecting an explicitly targeted operation.

## Capture and baselines

A checkpoint contains working-file content while leaving the user's workspace and
staging decisions intact.

- Capture tracked modifications/deletions and non-ignored untracked files by default.
  Support tracked-only capture and explicit ignored inclusion; these options conflict.
  Ignored files remain excluded by default.
  Staged additions count as tracked; working-file content wins over staged content.
- Support optional file/directory selection relative to the supplied invocation
  directory, including names with spaces. Reject paths outside the repository.
  Selecting a path must not silently override ignored/untracked capture policy.
  Each explicit selection must match at least one eligible path, including deleted
  tracked files; reject unmatched selections even alongside valid ones. No paths
  select the whole repository. Unchanged eligible files remain valid selections;
  compare the complete proposed snapshot with its parent to determine no change.
- Preserve HEAD, normal branch refs, index bytes, staged content, and working files.
  A partially staged file must remain partially staged with identical staged and
  working contents after capture. Do not use stash/reset/checkout to achieve this.
- An unchanged capture succeeds with a structured no-change result and does not
  advance the ref. Permit an explicit baseline checkpoint even when there are no
  edits so a caller can mark a deliberate starting point.
- Return whether a commit was created, its full object ID when applicable, its parent,
  the checkpoint ref, and the work-branch HEAD used for the operation.
- Accept a message and define a useful default. New checkpoints show the human
  message first in ordinary Git logs and put strict versioned ownership metadata
  after it. Preserve exact caller messages, including multiline/trailing-newline
  text, in API history; support existing version-1 checkpoints without rewriting
  them. Resolve commit identity using a documented policy. Fixed Git authorship
  does not identify an agent/session; callers keep session identity in their records.
  Do not unexpectedly invoke signing prompts or user hooks.
- Use an isolated temporary index and conditional ref publication. A competing
  writer must produce a recognizable conflict instead of being overwritten. Clean
  up temporary files on errors and cancellation; harmless unreachable Git objects
  may remain until normal Git garbage collection.
- A successful result means the ref update completed. Document that a process killed
  around publication may require the caller to inspect the ref before retrying.
  Do not promise an atomic snapshot while other processes modify working files.

Settled capture policies are specified in [architecture](architecture.md): snapshots
start from HEAD; unselected paths retain HEAD content; parent stacks roll over after
HEAD changes; strict versioned metadata guards owned refs; a fixed noninteractive
identity requires no user Git identity; tracked-only capture conflicts with ignored inclusion.
Sparse worktrees, unmerged indexes, and gitlinks are explicitly unsupported for capture.

## History, status, and review data

- Report the branch's current HEAD, checkpoint ref/tip, and whether its checkpoint
  history applies to the current branch state. Distinguish no history from an error.
- List checkpoints with full object IDs, parents, messages, and timestamps in a
  documented order. Support a limit so consumers need not load an entire history.
- List branch checkpoint refs, including refs whose normal branches no longer exist.
- Expose retained reflog entries so earlier history can be discovered after rollover.
  Deduplicate or identify repeated entries explicitly. Do not promise that reflogs
  retain objects indefinitely or survive deletion of the owning ref.
- Inspect a selected checkpoint's changed paths and change summary. Support comparison
  of two explicit revisions and a textual Git diff for callers that need it. Return
  structured changed-file data separately from display output.
- Provide read-only access to a file at a selected checkpoint, including binary bytes.
  Restoring files into the worktree is outside initial scope.
- Support path filtering of history/comparison data where useful for review. Filtering
  a review does not change the scope of an earlier checkpoint.
- Use ordinary Git revisions and full object IDs so callers can invoke their preferred
  reviewer directly without a custom range or session format.

## Deletion and cleanup

Delete a selected checkpoint ref without changing normal branches, HEAD, staging,
or working files. Use the expected current ref value to avoid deleting a concurrent
update. Define missing-ref behavior consistently in the API and CLI.

List orphaned refs before cleanup, and let callers choose explicit deletion targets.
Cleanup returns the refs deleted and any failures; do not imply a multi-ref operation
was atomic if some deletions succeeded. Never run garbage collection or prune Git
objects automatically. The library has no confirmation prompts.

## CLI

Design the command surface around the library operations: capture/baseline, status,
history, ref listing, inspection/comparison, deletion, and cleanup. Final command
names are decided during CLI work, without legacy aliases or config requirements.

- No arguments shows help. Writes require explicit commands; help/version work
  outside a repository. Provide an explicit repository-directory option.
- Offer clear human output and JSON for structured results, including full object IDs.
  Keep diagnostics on stderr and data on stdout. Document the JSON format.
- An unchanged capture exits successfully. Errors exit nonzero; document stable exit
  categories without exposing raw Git exit codes as the entire application contract.
- Destructive commands require explicit confirmation or a noninteractive flag.
  Scripts must never hang waiting for a prompt. Define behavior when stdin is not a
  terminal and test it. Avoid terminal detection dependencies unless actually needed.
- Do not advertise unsupported signing, configuration, or editor integrations.

## Embedding and documentation

Include a runnable external Go consumer: open a repository, create a baseline,
capture edits, inspect history, select two SHAs, and obtain their changed-file summary.
The example should demonstrate cancellation/error handling and a no-change result.
It must use the public API rather than internal packages or CLI output parsing.

Document agent-session integration with whole-workspace start/end captures and
caller-owned turn records. Validate unchanged turns, intervening captures and normal
commits using explicit review endpoints; distinguish snapshot intervals from agent
authorship. Cover host locking, deadlines, ambiguous failures, retention and legacy
cutover without implementing a host workflow engine. See
[agent-session integration](agent-sessions.md).

Document prerequisites, build/test commands, library examples, CLI usage, capture
policies, ref/history lifecycle, and limitations. Explain that checkpoints are local
and ephemeral rather than a backup system. Ignored files are excluded by default,
but capture policy does not detect secrets in included files.

## Validation and completion

Use original tests in temporary repositories with isolated Git configuration and
identity. Git's own commands verify object contents and storage invariants.

| Scenario | Required evidence |
| --- | --- |
| Baseline, changes, unchanged capture | Commit creation/no-change results and ref behavior |
| Several checkpoints and normal branch advancement | Parent chain, active-history policy, retained reflog entries |
| Tracked edits/deletions, staged additions, untracked/ignored files | Exact captured tree and capture-option behavior |
| Selected files/directories, subdirectory invocation, spaces | Correct path resolution and preservation of unselected content |
| Partially staged file | Identical real index bytes, staged content, HEAD, and working contents |
| Multiple branches and orphaned refs | Correct isolation, listing, deletion, and cleanup outcomes |
| History, comparison, file read | Structured metadata, changed paths, diff, and exact file bytes |
| Invalid states, cancellation, competing updates | Clear errors and no unintended state changes |
| CLI scripts and public Go consumer | Usable output/exit behavior and no dependency on internal APIs |

Keep tests focused on real workflows and meaningful failure cases. Routine tests
must not need another checkpoint tool, a C++ toolchain, external services, or user
repositories. Run formatting, vet, tests, and build; run race tests when supported.
Perform an independent code review for correctness, readability, modularity, and
unnecessary complexity, then resolve findings before task commits.

Support Linux and macOS only; record actual test platforms separately from intended
support. Windows is outside the project scope. The module is `github.com/anzax/go-git-wip` and the minimum supported Git
version is 2.30. Task records identify actual validation; macOS execution must not
be claimed unless separately tested.

Do not build a daemon, watcher, workflow engine, review UI, editor plugin, hook
installer, automatic restore operation, or exhaustive Git feature matrix in this scope.
