---
title: Include untracked files in checkpoints by default
status: done
updated: '2026-10-10'
---

# Outcome

Default checkpoints include tracked and untracked working files. Newly created files
are ordinary work and must appear in agent-session snapshots without an extra flag.
Ignored files remain excluded unless explicitly requested.

## Scope and acceptance

- [x] Make zero-value `CaptureOptions{}` include tracked and non-ignored untracked
  files. Replace `IncludeUntracked` with `TrackedOnly`; retain `IncludeIgnored`.
- [x] Change CLI behavior consistently:

  | Invocation | Eligible content |
  | --- | --- |
  | `save` | Tracked and non-ignored untracked files |
  | `save --tracked-only` | Tracked files only, including staged additions |
  | `save --ignored` | Tracked, untracked, and ignored files |

- [x] Replace the old `--untracked` opt-in with `--tracked-only`. Reject conflicting
  `TrackedOnly`/`IncludeIgnored` options and `--tracked-only --ignored` clearly.
- [x] Keep path selection as a restriction on eligible content: explicitly selecting
  an ignored file does not include it without ignored inclusion. Selected untracked
  files are included by default.
- [x] Apply the same eligibility rules to forced baselines and ordinary captures.
  Preserve HEAD, normal refs, real index bytes, and working files; keep existing
  snapshot-base, parent-chain, and no-change semantics.
- [x] Update API comments, CLI help, requirements, architecture, library/CLI guides,
  README, and the external consumer to use the new contract. Remove obsolete
  instructions requiring untracked inclusion for ordinary agent snapshots.
- [x] Add or update realistic tests proving a newly created file appears in a default
  snapshot and its comparison, `TrackedOnly` excludes it, ignored files require
  opt-in, path selection respects policy, and repeated capture returns no change.
  Cover CLI JSON/error behavior and baseline eligibility as well as the public API.
- [x] Review the contract change and implementation and run the checks below.
- [x] Prepare this reviewed task for its separate authorized commit.

## Verification

Passed on Linux/arm64 with Go 1.27.2 and Git 2.56.0:

- `make check`: formatting, vet, real Git API/CLI tests, external consumer vet/run.
- `make build`: CLI executable builds.
- `make race`: API/CLI tests pass with the race detector.
- `git diff --check`: no whitespace errors.

The new tests verify default added-file tree contents and `Inspect` data, tracked-only
exclusion with staged additions retained, independent ignored opt-in, baseline
eligibility, literal selected paths, no-change/ref stability, JSON and error behavior,
and HEAD/normal ref/index/staged content/working file preservation. The external
consumer adds a new file without options and compares it alongside a tracked edit.
Independent contract/code review found no correctness or scope issues.
macOS and Git 2.30 have not been execution-tested.

## Resume

Implementation and verification are complete; changes are frozen for the integration
owner's separate authorized task commit.
The API replaces `IncludeUntracked` with `TrackedOnly`; CLI replaces `--untracked`
with `--tracked-only`. Zero-value capture includes non-ignored untracked files;
`IncludeIgnored`/`--ignored` works independently and conflicts with tracked-only.

HEAD-based independent partial snapshots, parent/ref/no-change behavior, and existing
explicit-selection validation are unchanged. Selection validation is the next linked
[task](../20261010-1948-validate-capture-selection/task.md). No work has begun on it.
The existing trailing README and package-doc demonstration comments were preserved
exactly and are unrelated to this task.
