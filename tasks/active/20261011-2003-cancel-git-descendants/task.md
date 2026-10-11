---
title: Stop Git descendants when a library operation is canceled
status: review
updated: "2026-10-11"
---

# Stop Git descendants when a library operation is canceled

## Outcome and scope

On supported Linux/macOS hosts, each library Git command runs in its own process group. Context cancellation/deadlines while Git remains active send SIGKILL to that group, stopping Git and ordinary filter descendants without signaling the caller's group. Keep context error identity and the existing 100 ms output-pipe wait bound. Descendants deliberately detached into another group, or surviving a leader that exited before cancellation was observed, remain outside the termination guarantee; publication can still complete before a canceled call returns.

## Evidence and implementation

- Source-inspected on 2026-10-11 at f701f53: `git.go` used `exec.CommandContext` with default direct-child kill and `WaitDelay=100ms`. The user approved repairing descendant cancellation in the shared library before the integrating consumer changes its hook path.
- Runtime-tested on 2026-10-11: before the runner change, `go test -run '^TestCaptureCancellationStopsFilterDescendants$' -count=1 -v .` failed in both `cancel` and `deadline`: the real Git clean-filter process or its ordinary shell descendant survived. PID-targeted test cleanup stopped both; short-lived sleep children exit naturally. The same regression passes with the repair.
- `git_process_unix.go` configures `Setpgid=true` and a custom `Cancel`: first check the process is still available, then send SIGKILL to its negative PID group. An absent group maps to `os.ErrProcessDone`; a reaped leader returns that error before any group signal. This follows the standard-library cancellation contract and creates no shell commands or new dependencies.
- `git_cancel_test.go` exercises real filter cancellation/deadlines, caller-group sibling survival, process-group identity, exit before cancellation, absent groups, and output-pipe waiting after ordinary command exit. The capture regression checks exact partially staged index bytes, working files, HEAD, existing checkpoint tip and private-index cleanup.
- `docs/requirements.md`, `docs/architecture.md` and `docs/library.md` now state cancellation behavior, detached-descendant limits, and the case where the Git leader exits before cancellation is observed. Existing publication ambiguity guidance remains applicable.

## Acceptance

- [x] A disposable Git clean-filter reproduction demonstrates current descendant survival without leaving executing processes behind.
- [x] Cancellation/deadline stops ordinary descendants and preserves the real index, HEAD and working files; errors retain context identity.
- [x] Existing library checks, external consumer and focused race tests pass; macOS execution is explicitly unverified.
- [x] Library documentation explains cancellation and residual uncertainty after publication.
- [x] Coordinating agent reviewed the helper/tests and clarified the leader-exit limit.
- [ ] User acceptance.

## Verification

Linux/arm64, Go 1.27.2, Git 2.56.0 on 2026-10-11:

- `make check && make build` — passed, including root/CLI tests, vet, formatting check and public external-consumer execution.
- `CGO_ENABLED=1 go test -race -run 'TestCaptureCancellationStopsFilterDescendants|TestGitProcessGroupAndExitBeforeCancellation|TestGitCancellationAbsentGroup|TestRunnerWaitDelayAfterCommandExit|TestRunnerCancellationDuringCommand|TestCaptureCancellationCleansPrivateIndex' -count=1 .` — passed.
- `CGO_ENABLED=1 go test -race -run '^TestRunnerWaitDelayAfterCommandExit$' -count=1 .` — passed after review strengthened the already-exited leader assertion.
- `GOOS=darwin GOARCH=arm64 go test -c -o /tmp/go-git-wip-darwin-cancel.test .` — cross-compiled successfully; temporary binary removed. This is compile evidence only: macOS execution and the minimum Git version remain unverified.

## Review and limitations

Coordinating review identified the leader-exit boundary: after Git has exited before cancellation is observed, same-group descendants holding output pipes may survive the bounded wait. The runner checks the process before signaling to avoid a stale group signal after reaping; safe stronger guarantees would require additional process lifetime machinery. The output-pipe test demonstrates and cleans up this residual case. Scope remains ordinary filter cancellation while Git is active; no subreaper, process-tree enumeration or group keeper is introduced.

## Resume

Coordinator review and independent cancellation regressions passed. The integrating consumer also passed its full checks, packaged smoke, 23 Chromium tests and focused race checks against this checkout through an untracked external Go workspace. User acceptance and publication remain pending. Obtain explicit publication authorization before pushing; consumers must verify the published module before pinning it.
