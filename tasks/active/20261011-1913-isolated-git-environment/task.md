---
title: Allow embedding callers to isolate inherited Git environment
status: review
updated: "2026-10-11"
---

# Outcome

Allow an embedding application to strip all inherited `GIT_*` settings for an explicitly selected repository without changing process-global environment or copying library Git operations. The alt-sbs native utility integration needs this stricter policy before replacing its repository-identity implementation with public `Open`/`Info` calls.

## Evidence and proposed scope

Source-inspected at `0f8d32dad150230ede08f6960a2b5495782afa8d`: `gitEnvironment` strips known routing/config-injection variables but retains other `GIT_*` settings, including tracing destinations and configuration-file overrides. `Open` provides no per-repository environment policy. Preserving those variables is current behavior; this task proposes an explicit isolation option for callers needing stronger read-only guarantees.

Prefer a small options-based open API while retaining `Open` defaults. Store only the selected policy on the repository, apply it to discovery and every subsequent Git call, and preserve library-owned identity/index/safety overrides. Do not expose a generic arbitrary command executor or mutate global environment. The maintainer approved fixing the library first. Use additive `OpenWithOptions(ctx, directory, OpenOptions{IgnoreInheritedGitEnvironment: true})`; retain `Open` defaults.

## Scope and acceptance

- [x] Public opt-in isolation strips all inherited `GIT_*` settings from discovery and subsequent operations while preserving non-Git environment and library-owned overrides.
- [x] Default `Open` behavior remains compatible; options and concurrency behavior are documented.
- [x] Disposable fixtures prove tracing cannot create an inherited destination file and inherited config/routing overrides cannot affect isolated operations.
- [x] Capture still uses its private index and fixed identity; context cancellation and existing read-only preservation tests pass.
- [x] Run `make check`, `make build` and independent review; record exact results and limitations.

## Verification

Runtime reproduction on Linux/arm64 with Go 1.27.2: a disposable external consumer called `Open` then `Info` with `GIT_TRACE` pointing to a temporary file. Both calls succeeded and created a 460-byte trace containing five subprocess records. Temporary fixtures were removed. The consumer's published dependency currently resolves to `v0.0.0-20261010204317-0f8d32dad150`. A changed library commit must be available through the public module source before the consumer can pin it reproducibly without a local replace.

## Resume

The approved API is implemented and locally verified. Obtain authorization to publish the reviewed commit, then pin that public revision in the alt-sbs adapter and rerun consumer verification without the temporary Go workspace. Publication remains a separate authorized action.

## Implemented and verified

`OpenWithOptions` copies an immutable opt-in policy into the repository; `Open` retains zero-option behavior. Filtering applies before all library-owned safety/index/identity overrides in discovery and subsequent operations. Ordinary non-Git environment and configuration files remain available. No process-global environment mutation or generic command-runner API was introduced.

A GPT-6.1 Sol implementation agent at high effort completed the bounded change. The coordinator independently reviewed production code, tests and docs and reran `make check build`: formatting, vet, all Go tests, external consumer vet/example and executable build passed. Focused isolation tests also passed. Consumer integration passed Go tests/vet/race and full application checks, smoke and 23 Chromium tests via a temporary external Go workspace. Local platform: Linux/arm64, Go 1.27.2, Git 2.56.0; macOS and minimum Git version were not execution-tested.
