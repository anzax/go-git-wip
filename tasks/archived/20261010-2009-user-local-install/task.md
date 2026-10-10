---
title: Install the CLI into a user-local bin directory
status: done
updated: '2026-10-10 20:10 CEST'
---

# Outcome

Make `make install` place the freshly built CLI in `$HOME/.local/bin` by default,
with an explicit `BINDIR` override and a printed destination.

## Scope and acceptance

- [x] Depend on the phony build target and install with mode 0755 into quoted `BINDIR`.
- [x] Document the default destination, PATH requirement, and destination override.
- [x] Verify installation into a temporary directory containing spaces and installed help/version.
- [x] Run `make check` and `make build`; review the focused changes.

## Verification

Validated with Go 1.27.2 and Git 2.56.0 on Linux/arm64:

- `make install BINDIR=...` succeeded in an isolated temporary directory containing
  spaces. Installed mode was 0755, `--help` listed commands, and `--version` returned
  `dev`. The temporary directory was removed.
- `make check` passed vet, tests, the external consumer workflow, and formatting.
- `make build` produced `bin/go-git-wip`; `git diff --check` passed.
- Focused diff review confirmed fresh builds, quoted destinations, and matching
  README instructions. Unrelated README and doc.go edits were preserved.

Final review passed. Default `make install` also succeeded; shell lookup resolved
`go-git-wip` from the user-local bin directory and `--version` returned `dev`.
No shell configuration was changed. macOS execution is not tested.

## Resume

Complete: implementation, review, and installation validated. This destination
policy replaces the Go-default installation
described in the [earlier task](../20261010-1945-make-install/task.md).
