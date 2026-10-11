---
title: Discover checkpoints with bounded batched reads
status: review
updated: "2026-10-12"
---

# Discover checkpoints with bounded batched reads

## Outcome and scope

Provide an embedding read operation over explicit deduplicated full commit roots, counting every visited candidate (including unrelated commits) against a positive budget. Return validated checkpoints and explicit incompleteness/failures. Consumers own roots, task filtering, fingerprints and presentation order; no cache, persistent index, pagination session or path comparison is required. Capture and retention policy remain outside this task.

The approved contract is `Repository.Discover(ctx, DiscoveryOptions{Roots, CandidateLimit}) (DiscoveryResult, error)`, with result fields `Checkpoints`, `Visited`, `Incomplete` and `Failures`. `DiscoveryFailure` identifies `Commit`, `Message` and the underlying Go `Err`. Missing/noncommit roots and malformed owned candidates produce explicit failures; command failures and cancellation return an incomplete result plus error. Malformed option IDs fail as invalid options. Discovery walks ordinary Git ancestry once and batches object bodies and validation dependencies instead of spawning per-candidate commands. The [bounded output task](../20261011-2049-bounded-read-output/task.md) supplies hard per-command collection caps.

## Acceptance

- [x] Deduplicate overlapping explicit roots; unrelated and sparse histories consume the candidate budget; use one lookahead to distinguish an exhausted walk.
- [x] Reuse batched metadata/bodies for owned storage and local parent-stack validation; no per-candidate processes or path diffs.
- [x] Surface malformed/missing objects and output/cancellation failure explicitly while preserving HEAD, refs, index and working files.
- [x] Document count/byte bounds, dependency lookahead and residual operation costs; record `make check`, `make build` and race results.

## Verification and resume

Implemented in `discovery.go`, public types and the shared `parseCheckpoint` reader. `discovery_test.go` validates overlapping and duplicate roots, exact and sparse budgets, version-1/version-2 messages, malformed ownership, missing/noncommit roots and bases, unavailable ancestors, immediate parent-stack validation outside the window, SHA-256 IDs, active/pre-canceled operations and aggregate batch output overflow. A 24-checkpoint listing uses at most six Git processes, with no diff or per-candidate ref validation. Ref-name validation is cross-checked against real Git. Read tests assert preserved HEAD, refs, real index bytes and working files.

`make check`, `make build`, `make race`, and focused discovery/output checks passed on Linux/arm64 with Go 1.27.2 and Git 2.56.0. macOS and minimum-version Git execution were not tested. Root type checks, immediate checkpoint parent bodies/base metadata and a single candidate-ID lookahead are excluded from `Visited`; per-command byte caps cover their collection. No aggregate heap or Git-process memory bound is claimed. An incomplete window establishes local checkpoint validation, not validity of all older ancestry. Owning requirements, architecture and library guides document the boundary. Coordinating review completed. The user authorized local library commits on 2026-10-12; publication remains unauthorized.
