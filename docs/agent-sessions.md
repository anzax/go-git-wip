# Agent-session turn traces

Capture the whole workspace at explicit turn boundaries and store the returned
object IDs in the host application's session records. A checkpoint chain is Git
storage history, not an agent-turn journal: unchanged turns create no commit,
other writers can add checkpoints, and normal commits start a new stack.

The library supplies snapshots and comparisons. The host supplies hook execution,
session identity, durable trace records, locking, deadlines, and review integration.
There is no need to add an agent runtime or workflow engine to this package.

## Capture policy and boundaries

Open the intended working tree explicitly. Use the same options for every session
boundary: no path selection, tracked and untracked content included, ignored files
excluded. The current API makes that the default; spelling the policy out in a
wrapper makes the integration easy to review:

```go
result, err := repository.Capture(ctx, wip.CaptureOptions{
    Paths:          nil,
    TrackedOnly:    false,
    IncludeIgnored: false,
    Baseline:       baseline,
    Message:        label, // For example: "session s1 turn 2: end".
})
```

An empty `Paths` captures the whole repository, even if `Open` received a
subdirectory. New non-ignored artifacts are included automatically. Ignored files
remain excluded; this is an eligibility policy, not a secret scanner. Capture needs
an attached local branch with a commit; other capture limitations are listed in the
[library guide](library.md). Explicit selections that match nothing eligible are
errors, not a way to request an empty checkpoint.

Use `Baseline: true` once before session work begins. It forces a starting commit,
including any eligible uncommitted work already present. Those existing edits then
belong to the baseline rather than the first turn's changes. Baseline does not clear
history or reset the workspace.

For each turn:

1. Run the host's existing preflight checks and acquire its capture lock.
2. Capture and persist a start endpoint, then release the capture lock.
3. Run the turn.
4. Capture and persist an end endpoint under the same capture lock, recording the
   agent's actual outcome separately from capture success or failure.
5. Pass the stored start/end IDs to the reviewer; attach feedback to that same scope.

The first turn can reuse a freshly created session baseline as its start if nothing
can write between them. Similarly, reusing one turn's end as the next start is valid
only when intervening writes are excluded. With only an after-turn hook, the honest
description is changes since the previous successful hook, not necessarily changes
made by this particular turn.

```text
Time       Host trace boundaries                  Other activity
  |
  +-- B    session baseline
  +-- S1   turn 1 start
  +-- E1   turn 1 end
  |                                               manual edit + capture X
  +-- S2   turn 2 start (may reuse X's SHA)
  +-- E2   turn 2 end (same SHA if unchanged)
  |                                               normal Git commit H2
  +-- S3   turn 3 start (may be H2 itself)
  +-- E3   turn 3 end (new checkpoint stack)
```

These labels describe observations, not necessarily distinct Git commits.

## Store explicit endpoints

After a successful capture, derive the effective snapshot ID as follows:

```go
if err != nil {
    return err // Do not use the result as a successful trace endpoint.
}
snapshot := result.Commit
if !result.Created {
    snapshot = result.Parent
}
```

`Created=false` is normal: persist the turn record anyway. Its endpoint can refer
to an existing checkpoint or an ordinary HEAD commit. Do not use an empty `Commit`,
the current ref tip read later, or a guessed parent of the next checkpoint instead.

A small caller-owned record is enough:

| Field | Meaning |
| --- | --- |
| Session ID and turn ID | Host-assigned identities, independent of Git authorship |
| Working-tree identity/root | Which checkout the trace belongs to; distinguish linked worktrees |
| Start/end endpoint | Full object ID, observed HEAD, `Created`, and capture completion timestamp |
| Turn outcome | Started, completed, interrupted, or failed, as reported by the agent host |
| Capture outcome | Successful endpoint or failed/ambiguous attempt with error category and time |

Persist the start record before running the turn. Allow an absent end endpoint for
an unfinished or failed capture; never store a zero endpoint as a successful result.
If policy permits the agent to continue after capture failure, mark the trace as
incomplete. A later snapshot cannot reconstruct the missed boundary. Timestamps
record observations; they are not atomic filesystem snapshot times.

Keep trace records outside the captured working tree so recording a trace does not
itself become the next turn's untracked change. In the runnable example records live
in memory and print to stdout; a real host must persist them. Human commit subjects
make generic Git views useful, but are labels, not unique session or operation IDs.
The fixed checkpoint author also does not identify the agent.

## Review exactly the recorded scope

Use `Compare` with explicit endpoints, including when HEAD advanced between them:

```go
turnDiff, err := repository.Compare(ctx, startOID, endOID, wip.CompareOptions{
    IncludePatch: true,
})
```

Compare the session baseline with its final successful endpoint for the net session
diff. Pass those same IDs to an external review tool and keep them attached to the
feedback record. Do not substitute `Inspect(endOID)`: it compares against that
checkpoint's Git parent, which may not be the turn's recorded start. `Compare` also
accepts ordinary commit endpoints that `Inspect` does not treat as checkpoints.

Filter the comparison with `CompareOptions.Paths` when reviewing a task's files;
keep turn capture unfiltered. Review paths are relative to the directory supplied
to `Open`. Independent HEAD-based partial captures are useful elsewhere, but are
not cumulative workspace snapshots for agent hooks.

A diff reports final content differences between observations. It does not reveal
edits made and reverted within a turn. Concurrent manual edits or other agents' work
can appear in it. A start/end pair narrows the time interval; it does not establish
exclusive authorship. Use an isolated workspace or coordinate all writers when that
attribution matters. A whole-session diff can include changes made between turns.

## Host coordination and failures

Preserve the host's existing hook checks, locking, deadline budgets, and output/error
protocol. Invoke the library directly behind a host command; the library prints
nothing and does not prompt. Keep structured host output on stdout and diagnostics
on stderr rather than embedding CLI text parsing. Use `errors.As` for `*wip.Error`
and `errors.Is` for cancellation/deadline errors.

Serialize cooperating capture hooks across reading the prior state, capture, and
recording the result. Coordinate writers to the same checkpoint ref, including linked
worktrees sharing that branch's ref. This lock does not prevent an editor or agent
from changing files while Git reads them. Workspace isolation or a broader host
write policy is separate; the library does not promise an atomic live filesystem
snapshot. Conditional publication still detects competing checkpoint ref updates.

Pass a bounded child context to each capture and comparison. On `KindConflict`,
record the failure and re-establish the intended boundary before retrying; do not
silently substitute another writer's tip. Capture and journal persistence are not
one transaction. Record an attempt ID and prior observed ref state if recovery from
a crash between them matters to the host.

Cancellation, timeout, process termination, or a late error can occur after ref
publication. Mark that outcome uncertain, then inspect `Status`, retained `Reflog`
entries, and `History` under the host's coordination policy before retrying. A unique
attempt label can help correlate candidates but does not make capture idempotent.
Do not automatically treat the newest tip as your successful endpoint. If attribution
cannot be established, preserve the incomplete record and start a new observed
boundary. Avoid unbounded retries and do not assume failure means no ref changed.

Recorded SHA strings alone do not retain Git objects. Rollover leaves old stacks
subject to reflog expiry and garbage collection; deleting a ref may delete its log.
If review traces must outlive those policies, the host must retain the referenced
objects through its own managed refs or archival policy. No automatic permanent
retention or cleanup belongs in the normal turn hook.

## Cutover and validation

For a controlled replacement of a legacy writer using `refs/wip/<branch>`:

1. Stop legacy hooks and other writers; verify they cannot recreate refs.
2. Inspect existing refs and obtain approval for discarding any legacy history.
3. Remove only approved legacy refs with an expected-tip Git operation. The library's
   ownership-aware `Delete` deliberately rejects unrelated legacy metadata.
4. Enable the new host wrapper and establish a baseline with the agreed policy.
5. Exercise a changed turn, an unchanged turn, explicit-SHA review, and feedback
   attached to the recorded scope; verify the host's cancellation/error behavior.

Do not run broad deletion as part of a routine hook. A separate namespace is needed
if the two incompatible writers must coexist; it is not necessary for this stopped-
writer cutover. This project's own version-1 checkpoints remain readable alongside
version 2 and do not require deletion for the message-format update.

Run `make example` to exercise the [session example](../examples/workflow/session.go).
It reuses the disposable external consumer repository and validates a baseline with
pre-existing work, changed and unchanged turns, an intervening capture, and a normal
commit/stack rollover. It checks per-turn and whole-session comparisons and prints
caller-owned trace records. `make check` vets and executes it as well.

The example uses only the public library and standard packages. It installs no hooks,
runs no reviewer, and implements no durable journal or host locking. Live host
capture-to-review-to-feedback validation remains the integrating application's work;
passing this example does not claim that deployment was tested.
