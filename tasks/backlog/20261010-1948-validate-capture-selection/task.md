---
title: Reject capture selections that match no eligible paths
status: done
updated: '2026-10-10'
---

# Outcome

An explicit typo, excluded file, or old-style positional message must not silently
produce a HEAD-based snapshot or a misleading no-change result. No path arguments
continue to mean capture the whole repository.

## Scope and acceptance

- [x] Validate each explicitly supplied path against the eligible capture paths;
  reject a selection matching nothing with `KindInvalidOption` and a diagnostic
  identifying that selection. Mixed valid and unmatched selections also fail.
- [x] Evaluate eligibility before change detection. Existing eligible paths remain
  valid regardless of content changes; no-change is determined by comparing the
  complete proposed snapshot with the chosen parent.
- [x] Preserve literal file/directory matching, subdirectory invocation, and selection
  of deleted tracked files or directories containing deleted tracked descendants.
- [x] Respect capture inclusion options: an ignored-only selection without ignored
  inclusion fails clearly. Coordinate untracked and tracked-only behavior with the
  [default-untracked task](../20261010-1944-default-untracked/task.md).
- [x] Validate before publishing a checkpoint. A rejected selection leaves refs,
  HEAD, real index, and working files unchanged, including when an older checkpoint
  contains changes that a fresh HEAD-based snapshot would otherwise remove.
- [x] Add focused real-Git tests for a typo, mixed valid/invalid selection, an old-style
  positional message, ignored-only selection, unchanged tracked file, and deleted
  tracked paths. Verify CLI exit 2 and JSON diagnostics as well as the library error.
- [x] Update API comments, requirements, architecture and CLI/library guides so they
  distinguish an omitted selection from an explicit selection with no eligible match.

## Verification

Passed on Linux/arm64 with Go 1.27.2 and Git 2.56.0:

- `make check`: formatting, vet, real Git API/CLI tests, external consumer vet/run.
- `make build`: CLI executable builds.
- `make race`: API/CLI tests pass with the race detector.
- `git diff --check`: no whitespace errors.

Tests cover unmatched/mixed/positional-message/ignored/tracked-only/literal/empty-dir
selections, forced baseline rejection, and normalized subdirectory diagnostics.
A nonexistent temporary-directory root confirms rejection precedes private-index
allocation. Rejections preserve an older checkpoint tip, HEAD, normal branch ref,
real index bytes, staged content, and working files. Deleted tracked files/directories
remain selectable. An unchanged eligible selection can create a HEAD restoration
when the preceding checkpoint contains omitted changes; repeating that snapshot is
no-change. Omitted selection still works with an empty eligible set. CLI diagnostics
use exit 2, `invalid_option` JSON, and the offending selection.
Independent contract/code review approved without findings.
macOS and Git 2.30 have not been execution-tested.

## Resume

Complete. Implemented, reviewed, and verified. Capture validates every normalized
explicit selection against full eligibility before filtering, private-index creation,
or publication. Shared read-only path normalization/filter behavior is unchanged.
No-change still compares the complete independent HEAD-based snapshot to its parent.
See the next [readable checkpoint subject task](../20261010-1948-readable-checkpoint-subjects/task.md).
