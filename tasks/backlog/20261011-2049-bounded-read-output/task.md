---
title: Bound Git output for embedding read operations
status: ready
updated: "2026-10-11"
---

# Bound Git output for embedding read operations

## Context and outcome

Source-inspected on 2026-10-11 while integrating task history in alt-sbs: `git.go` buffers stdout and stderr in unbounded `bytes.Buffer` values. `ReadFile`, `Compare`, `Refs` and checkpoint metadata reads have no caller-selected byte cap. Context deadlines bound elapsed execution, not peak memory. A history count limit also does not bound one commit message, blob or patch.

The consumer can preflight selected task paths and blob sizes, but shared hard output limits belong in the library rather than a duplicate comparison implementation. Design a bounded-read option and explicit typed failure that never returns a successful truncated result. Preserve default behavior unless a deliberate compatibility decision changes it.

## Acceptance checks

- [ ] Agree public options and whether caps cover stdout, stderr and aggregate operation output.
- [ ] Bound subprocess collection before buffers exceed the selected limits; stop execution and preserve cancellation/error semantics.
- [ ] Exercise large blobs, commit messages, patches and stderr in disposable repositories, including output-limit cleanup and unchanged HEAD/index/worktree/refs.
- [ ] Document caller limits and any residual metadata/operation costs; run `make check build` and focused race checks.

## Resume and verification

No library implementation or API decision is authorized by this follow-up alone. Coordinate the public contract before implementation. Current consumer mitigation is scoped metadata/blob preflight and deadlines; it is not a hard library memory guarantee. This task records source inspection only; no new runtime test has been performed.
