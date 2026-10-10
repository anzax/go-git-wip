# Working on go-git-wip

Read README.md, the current task, and docs/requirements.md before implementation. Follow tasks/README.md for progress and handoff.

## Implementation

- Build an independent MIT-licensed Go checkpoint library with a thin CLI. Our
  requirements determine behavior; git-wip compatibility and quirks are not goals.
- Do not copy, translate, or adapt git-wip code, tests, comments, or documentation.
  Use our requirements and official Git documentation as implementation inputs.
- Keep reusable operations in public package `wip`; Cobra, formatting, prompts,
  and exit codes belong in internal/cli. Prefer small concrete types and functions.
- Invoke local Git with `exec.CommandContext` and argument arrays. No shell command
  construction, go-git, libgit2, or process-wide `os.Chdir` in production code.
- Checkpoints must preserve HEAD, normal branch refs, real index bytes, and working
  files. Use a private temporary index and conditional ref updates. Never use stash,
  checkout, reset, or clean as an implementation shortcut.
- Library methods accept context and return structured results/errors. No stdout,
  prompts, `os.Exit`, or implicit terminal/editor integration inside the library.
- Prefer standard library dependencies. Cobra is the CLI baseline; introduce other
  dependencies only for an actual requirement and check their licenses.

## Verification and coordination

- Write original tests from project requirements and realistic workflows. Use
  temporary repositories, isolated Git config/identity, and actual Git assertions.
  Never run destructive tests against this checkout or other existing repositories.
- Run `make check` and `make build` for code changes. Record checks actually run;
  distinguish scaffold verification from implemented checkpoint behavior.
- Review correctness, readability, modularity, and unnecessary complexity before
  completion. Keep tasks current and document material limitations.
- Software design and public contracts use GPT-6 Astra with medium reasoning.
  Mechanical implementation and validation use GPT-6.1 Sol with high reasoning.
  Run at most two concurrent subagents with focused requirements and Git documentation,
  disjoint writable scopes, and serialized integration/commits.
- Keep core docs, these instructions, and task resume points current as decisions
  change so work can resume reliably after context compaction.
- The module is `github.com/anzax/go-git-wip`. Support Linux and macOS only;
  validate on this Linux host and do not imply macOS execution was tested.
- Commit each completed, reviewed task separately when authorized. Preserve unrelated
  changes. Do not push, publish, install hooks, or modify other repositories as a
  side effect. Amend existing commits only when explicitly requested.

## Communication

Keep documentation self-contained; do not mention the user's other projects or
local paths. Credit git-wip as inspiration. Lead with the result, explain material
tradeoffs, and give a concrete next step. Be direct, calm, and technical.
