---
title: Implement independent Git checkpoint library and CLI
status: active
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
- [ ] Design and wire a small CLI with explicit write commands, human/JSON output,
  predictable noninteractive behavior, and help on no arguments.
- [ ] Write original scenario tests and verify objects/refs with Git commands.
- [ ] Add a runnable external Go consumer for baseline/capture/history/range selection.
- [ ] Review correctness, readability, modularity, and complexity; resolve findings.
- [ ] Update API/CLI docs and record successful `make check` and `make build` runs.

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

Astra settled design/contracts and reviewed the foundation; make check/build passed. Use disposable repositories, isolated Git configuration/identity, and
original tests from the behavior contract. Compare tree contents, ancestry, refs,
reflogs, and state preservation with Git itself. No C++ toolchain is required.

## Resume

Library operations complete and reviewed. Deliver CLI, external embedding workflow,
final documentation and end-to-end Linux validation.
