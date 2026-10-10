# Architecture

Status: implementation direction; exported operation types are not defined yet.

## Library first

Public package `wip` owns repository discovery, checkpoint capture, status/history,
revision comparisons, file reads, and ref deletion/cleanup. Methods accept context and an explicit repository location;
results carry object IDs and metadata. An unchanged capture is a normal result.
The library does not import Cobra or interact with a terminal.

Keep Git execution private and small: command directory, cancellation, argument
arrays, controlled environment overrides, captured stderr, and useful errors. Use
concrete implementation types instead of a general Git backend abstraction. Tests
exercise real repositories rather than a mock object database.

The CLI handles command parsing, human/JSON output, prompts, and exit codes. No arguments shows
help. Its UX can evolve independently from the library's operation names.

## Git transaction direction

Use documented Git plumbing to create snapshots:

1. Discover the repository, resolve its branch and HEAD, and inspect the checkpoint
   ref. Select the snapshot base and parent according to our documented policy.
2. Allocate a private temporary index and load the chosen tree with `read-tree`.
3. Apply working-tree changes according to explicit capture/path options using Git
   with `GIT_INDEX_FILE`. Never write the real index.
4. Run `write-tree`; identify a no-change result unless baseline creation was requested.
5. Create the checkpoint using `commit-tree`, then publish it through
   `update-ref --create-reflog` with an expected previous value. A competing update
   must fail without silently replacing the winner.
6. Remove the temporary index on every exit path.

Validate plumbing against official Git documentation during implementation. Specify
capture base, parent selection, identity, and existing-ref validation in requirements
before writing these steps. Snapshot content and parent choice are separate decisions.
Guard against inherited Git environment variables redirecting repository/index access.
Avoid implicit interactive signing or hooks in library operations.

## Embedding example

An application creates a baseline, performs work, saves a checkpoint, reads history,
and hands an explicit pair of SHAs to an external reviewer. The caller owns reviewer
processes, sessions, hooks, and task attribution. Filtering a review does not imply
filtering a checkpoint.

Normal commits and temporary checkpoint history have separate lifecycles. Document
how branch changes affect the active history and which older commits remain reachable.
Do not promise backup retention beyond Git's ref/reflog and garbage collection rules.

Keep workflow engines, watchers, diff renderers, and review-tool integrations outside
this library. Add the executable embedding example once the operations exist.
