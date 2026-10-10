---
title: Add a standard Go CLI install target
status: done
updated: '2026-10-10'
---

# Outcome

Install the CLI from this checkout with `make install`, using Go's normal binary
installation destination and an explicit `GOBIN` when supplied.

## Scope and acceptance

- [x] Add a phony `install` target running `go install ./cmd/go-git-wip`.
- [x] Explain installation and Go's `GOBIN`/`GOPATH` defaults in the README.
- [x] Verify installation into a temporary `GOBIN` and installed help/version.
- [x] Run `make check` and `make build`; review the focused changes.

## Verification

Validated with Go 1.27.2 and Git 2.56.0 on Linux/arm64:

- `make install` with `GOBIN` set to an isolated temporary directory succeeded;
  the installed binary's `--version` returned `dev` and `--help` listed commands.
  The temporary directory was removed after verification.
- `make check` passed vet, tests, the external consumer workflow, and formatting.
- `make build` produced `bin/go-git-wip`.
- Focused diff review confirmed the Makefile uses Go's standard installation
  behavior and the README accurately describes the destination and PATH setup.

No additional tests were needed for this build/documentation change. macOS
execution is not tested.

## Resume

Complete. Installation target and documentation verified; no remaining work.
