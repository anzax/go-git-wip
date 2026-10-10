---
title: Settle contracts and implement repository foundation
status: done
updated: '2026-10-10'
---

## Outcome

Explicit checkpoint policies and public API, safe local Git execution, repository discovery,
and original hermetic test fixtures. Module `github.com/anzax/go-git-wip`.

## Acceptance

- Astra medium settles contracts and design in core docs.
- Sol high implements foundation with real Git tests.
- Repository/environment isolation and cancellation verified.
- Review, make check, and make build pass before a focused commit.

## Verification

Astra medium settled contracts and independently reviewed the foundation; its
relative-executable-path finding was fixed. Hermetic real Git tests cover discovery,
subdirectories, detached/unborn/linked working trees, missing Git, environment
redirection, hooks/signing suppression, cancellation, and workspace preservation.
`make check` and `make build` passed on Go 1.27.2 / Git 2.56.0 Linux/arm64.

## Resume

Foundation complete. Continue [capture](../../backlog/20261010-1800-capture/task.md).
Contracts live in requirements/architecture/types.go. Runner helpers are private
`git`, `gitEnv`, and `gitExitCode`; each command uses an explicit directory.
