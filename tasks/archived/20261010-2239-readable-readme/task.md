---
title: Rewrite the README for everyday use
status: done
updated: '2026-10-10'
---

# Outcome

The [README](../../../README.md) introduces checkpoints in plain language and leads
with installation and a save/review workflow. Detailed API and storage contracts
remain in the linked guides.

## Scope and acceptance

- [x] Explain the purpose before implementation details.
- [x] Provide practical CLI and Go examples.
- [x] Retain capture semantics, key limitations, license, and inspiration credit.
- [x] Link detailed guides and verify local link targets.

## Verification

Reviewed examples and behavior against the CLI and library guides. All README
local link targets exist; `git diff --check` passed. Documentation-only changes;
Go tests and build were not rerun.

## Resume

Documentation complete and archived. No implementation changes.
