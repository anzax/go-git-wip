---
title: Bound Git output for embedding read operations
status: review
updated: "2026-10-12"
---

# Bound Git output for embedding read operations

## Context and outcome

Source-inspected on 2026-10-11 while integrating task history in alt-sbs: `git.go` buffers stdout and stderr in unbounded `bytes.Buffer` values. `ReadFile`, `Compare`, `Refs` and checkpoint metadata reads have no caller-selected byte cap. Context deadlines bound elapsed execution, not peak memory. A history count limit also does not bound one commit message, blob or patch.

The consumer can preflight selected task paths and blob sizes, but shared hard output limits belong in the library rather than a duplicate comparison implementation. Design a bounded-read option and explicit typed failure that never returns a successful truncated result. Preserve default behavior unless a deliberate compatibility decision changes it.

## Acceptance checks

- [x] Agree public options and whether caps cover stdout, stderr and aggregate operation output.
- [x] Bound subprocess collection before buffers exceed the selected limits; stop execution and preserve cancellation/error semantics.
- [x] Exercise large blobs, commit messages, patches and stderr in disposable repositories, including output-limit cleanup and unchanged HEAD/index/worktree/refs.
- [x] Document caller limits and any residual metadata/operation costs; run `make check build` and focused race checks.

## Resume and verification

Implementation authorized on 2026-10-12. The approved public contract adds `OpenOptions.MaxStdoutBytes` and `MaxStderrBytes` (`int64`), with zero preserving unlimited defaults and negative limits rejected. Limits apply separately to each Git command, including discovery; there is no aggregate operation or Git-process memory promise. The collector stops execution on overflow and returns `KindOutputLimit`, preserving caller context cancellation and private-index cleanup. Storage-error wrappers preserve this failure kind.

Implemented in `git.go`, `OpenOptions`, and repository policy fields, with typed-failure preservation through existing storage readers and capture publication validation. `git_output_test.go` covers collection/backing-array bounds, exact-limit blobs, larger blobs/patches/messages, stderr and discovery caps, zero/default and invalid options, cancellation identity and concurrent reads. `git_cancel_test.go` confirms overflow stops an active clean filter and its ordinary child, cleans the private index and preserves real workspace/ref state.

`make check`, `make build`, `make race`, and focused output/discovery checks passed on Linux/arm64 with Go 1.27.2 and Git 2.56.0. macOS and minimum-version Git execution were not tested. Output caps are per command, not aggregate heap or Git-process bounds; configured caps also affect captures and can require ref inspection if publication raced a failure. Owning requirements, architecture and library guides describe these contracts. Coordinating review completed; bounded traversal is tracked separately in [batched checkpoint discovery](../20261012-0035-batched-checkpoint-discovery/task.md). The user authorized local library commits on 2026-10-12; publication remains unauthorized.
