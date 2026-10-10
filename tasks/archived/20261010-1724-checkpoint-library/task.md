---
title: Implement independent Git checkpoint library and CLI
status: done
updated: '2026-10-10'
---

# Outcome

An MIT-licensed Go library for native Git checkpoints, with an optional CLI and a
runnable embedding example. Follow [requirements](../../../docs/requirements.md),
[architecture](../../../docs/architecture.md), and [development rules](../../../AGENTS.md).

## Scope and acceptance

- [x] Define capture-base/parent policies, baseline creation, existing-ref validation,
  identity, and path-selection semantics before exposing the operation API.
- [x] Implement private Git execution and repository discovery with cancellation.
- [x] Implement capture with a private index, explicit options, no-change results,
  reflogs, conditional ref updates, and preservation assertions.
- [x] Implement structured status/history, checkpoint inspection, file changes,
  explicit revision-pair comparison, historical file reads, ref listing/deletion,
  and orphan cleanup.
- [x] Design and wire a small CLI with explicit write commands, human/JSON output,
  predictable noninteractive behavior, and help on no arguments.
- [x] Write original scenario tests and verify objects/refs with Git commands.
- [x] Add a runnable external Go consumer for baseline/capture/history/range selection.
- [x] Review correctness, readability, modularity, and complexity; resolve findings.
- [x] Update API/CLI docs and record successful `make check` and `make build` runs.

## Execution slices

Create a focused task for each slice as work starts; commit each after review and
verification. This task tracks the overall outcome.

1. Behavior decisions, public API conventions, Git runner, and test fixtures.
2. Capture and its lifecycle/preservation tests.
3. History, inspection, deletion, and cleanup.
4. CLI, embedding example, and end-to-end validation/documentation.

Use GPT-6 Astra at medium reasoning for design/contracts and design review;
use GPT-6.1 Sol at high reasoning for mechanical implementation/validation. Limit concurrency to
two subagents with disjoint writable scopes; serialize integration and commits.
Supply focused project requirements and Git documentation.

## Boundaries

No full compatibility claim, copied/adapted upstream code or tests, general Git
backend framework, daemon, watcher, workflow engine, review UI, or hook installer.
Comparisons with another executable are optional design evidence, not release gates.
Module: `github.com/anzax/go-git-wip`. Choose and document the minimum Git version.
Support Linux and macOS only; validate on the Linux host.

## Verification

Completed in isolated commits: foundation `2003864`, capture `492e95f`, inspection
`b7414d2`, then CLI/embedding/documentation. Astra medium settled software design and
contracts and independently reviewed each implementation phase. Sol 6.1 high handled
mechanical implementation and validation with at most two active, disjoint agents.

Original temporary-repository tests use Git as the oracle for trees, ancestry, refs,
reflogs and exact index/worktree preservation. Coverage includes staged/untracked/
ignored files, partial captures, normal-HEAD rollover, ownership, conflicts, cancellation,
review data, binary reads, explicit deletion and partial orphan cleanup. CLI tests
exercise the built process, JSON/human output, exit categories and actual SIGINT.
The separate external Go module demonstrates the public API end to end.

Final `make check`, `make build`, `make race`, `git diff --check`, and local Markdown
link checks passed on Linux/arm64 with Go 1.27.2 and Git 2.56.0. Only Linux execution
is validated. macOS and the declared Git 2.30 minimum were not execution-tested.
All review findings resolved; core docs/instructions updated with settled contracts.

## Resume

Scope complete. Read maintained requirements, architecture, library guide and CLI
guide for future changes. Capture intentionally excludes sparse/unmerged/submodule
states; no restore engine, background service or workflow orchestration was added.
