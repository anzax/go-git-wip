---
title: Deliver CLI and embedding workflow
status: done
updated: '2026-10-10'
---

## Outcome and acceptance

Human/JSON commands, stable errors, explicit deletion confirmation, external public API consumer, end-to-end Linux validation and user documentation.
Follow [requirements](../../../docs/requirements.md) and [architecture](../../../docs/architecture.md).

## Verification

- Full thin Cobra adapter covers save/status/history/refs/reflog/show/diff/file/delete/cleanup.
- Human/JSON output, exact raw bytes, stable exit codes and explicit destructive flags tested.
- Compiled executable tests verify codes 0/1/2/3/130, conflicts, noninteractive deletion,
  and real SIGINT cancellation during a controlled Git clean filter.
- Separate `examples/workflow` Go module imports only the public package and stdlib;
  validates baseline/edit/no-change/history/two-SHA comparison/HEAD/cancellation.
- Astra medium independently reviewed design/contracts, modularity and correctness.
  JSON flag-value diagnostics and empty-patch documentation findings resolved.
- Final `make check`, `make build`, `make race`, `git diff --check` and local Markdown
  link validation passed. `make check` includes vet and execution of the external consumer.
- Linux/arm64 only: Go 1.27.2, Git 2.56.0. macOS and minimum Git 2.30 not executed.

## Resume

Complete. Core docs describe implemented behavior. No unfinished implementation work
in this scope; future API or storage changes should update contracts and focused tests.
