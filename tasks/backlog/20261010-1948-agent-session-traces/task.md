---
title: Document agent-session integration with explicit turn traces
status: done
updated: '2026-10-10'
---

# Outcome

Add a practical `docs/agent-sessions.md` guide and a verified public-API example for
embedding checkpoints behind agent hooks. Explain how to retain explicit per-turn
review endpoints without confusing Git parentage with agent-session history.

## Scope and acceptance

- [x] Document the smallest integration: a host command opens an explicit repository,
  captures whole-workspace state with untracked inclusion and ignored exclusion,
  and returns structured results under the host's existing error/output protocol.
  Incorporate the final [capture defaults](../20261010-1944-default-untracked/task.md).
- [x] Show a forced session baseline before work so pre-existing changes are starting
  state. Explain that capture operates on the whole repository even when Open receives
  a subdirectory; filter comparisons for review rather than filtering turn snapshots.
- [x] Describe start/end hooks for per-turn boundaries. Record a start snapshot before
  a turn and an end snapshot after it. Reusing the preceding end as the next start is
  valid only when intervening writes are excluded; an after-turn-only hook observes
  intervals between hooks, not necessarily one agent turn.
- [x] Provide a small caller-owned trace record: session ID, turn ID, repository/worktree,
  timestamps, start/end full object IDs, observed HEAD, capture-created/no-change state,
  and completion or capture-failure status. Do not infer session identity from authors,
  commit subjects, parent links, or reflog positions.
- [x] Show endpoint handling explicitly: use `Commit` when `Created=true`, otherwise
  `Parent`. An unchanged turn still gets a host trace record; no Git commit is required.
  Do not label a failed capture as unchanged or fabricate an endpoint on failure.
- [x] Demonstrate comparisons of stored start/end SHAs for a turn and baseline/final
  SHAs for a session. Keep explicit endpoints across normal HEAD advancement and stack
  rollover; endpoints may refer to ordinary commits as well as checkpoints.
- [x] State attribution and retention limits: simultaneous manual/agent writes can be
  included, transient edits within a turn are not captured, and recorded SHAs alone do
  not retain objects. Distinguish hook serialization from workspace isolation and
  document that durable retention is a caller policy beyond ephemeral reflogs.
- [x] Cover existing host checks, locking scope, deadlines/context cancellation,
  concurrent-publication conflicts, and preservation of stdout/stderr and exit/error
  conventions. For ambiguous cancellation around publication, explain reconciliation
  before retrying instead of assuming no ref update occurred. Avoid blind retry loops.
- [x] Document controlled legacy cutover without requiring another namespace: stop and
  verify old writers are disabled, inspect refs and obtain approval for discarding
  legacy history, remove only approved legacy refs, activate the new writer, and check
  capture-to-review-to-feedback. Our ownership-aware Delete API does not delete
  unrelated legacy refs; use a separately authorized, expected-tip Git operation.
  Explain that namespace separation matters when coexistence is required.
- [x] Extend the disposable external consumer or add a focused example validating a
  session baseline, changed turn, unchanged turn, stored endpoints, and comparisons
  across an intervening capture or normal commit. Use only the public API and stdlib.
- [x] Link the guide from README and the library guide. Keep documentation self-contained
  and generic; do not name other private projects or require a particular agent/reviewer.

## Verification

Completed against `a8efa93` after checking the preceding implemented contracts:
`1452b49` (untracked defaults), `345d1f3` (selection validation), and `a8efa93`
(readable version-2 subjects with version-1 reading support).

- Added [agent-session guide](../../../docs/agent-sessions.md), linked from README,
  library guide, requirements and development instructions.
- Added [session example](../../../examples/workflow/session.go) to the existing
  external consumer; `make example` and `make check` execute it automatically.
- The example verifies baseline treatment of pre-existing work, a changed turn,
  an unchanged turn after an intervening capture, a normal commit and stack rollover,
  explicit endpoint selection, and per-turn versus whole-session comparisons.
- Reviewed guide and example against `CaptureOptions`, `CaptureResult`, `Compare`,
  cancellation behavior and the current ownership/storage contract.
- `make check` and `make build` passed on Linux/arm64, Go 1.27.2, Git 2.56.0.
  This includes all existing tests, vet/format checks and the separate public consumer.
- Local Markdown links and `git diff --check` passed. No concurrent example code was
  added; no additional race run was needed for this documentation/example slice.
- Existing demo edits in README and doc.go were preserved. No host hooks, existing
  refs or reviewer processes were changed; live host integration remains unvalidated.

## Resume

Complete. Run `make example` for the executable trace demonstration. The integrating
application still owns its hook wrapper, persistent records, locking, retention and
live capture-to-review-to-feedback validation. No new public library API or workflow
engine was introduced. This slice is ready for use by the integrating application.
