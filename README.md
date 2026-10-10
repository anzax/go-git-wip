# go-git-wip

Save work-in-progress snapshots without changing your files, staging area, or
current branch. Review them later using the CLI, the Go library, or Git itself.

Checkpoints are stored as Git commits in a separate history for each branch. You
can save unfinished work without adding commits to your normal branch history.

Inspired by [git-wip](https://github.com/bartman/git-wip) by Bart Trojanowski.
This is an independent implementation with its own API and behavior.

## Install

Requires Go 1.26.2+ and Git 2.30+. Targets Linux and macOS; validation so far has
been on Linux only.

From this checkout:

```sh
make install
```

This builds `go-git-wip` and installs it into `~/.local/bin`. Make sure that directory
is on your `PATH`. To use another location, run `make install BINDIR=/your/bin`.

To try it without installing, run `make build` and use `./bin/go-git-wip`.

## Save and review your work

In a Git repository with at least one commit, while on a branch:

```sh
# Save a starting point, even if nothing has changed.
go-git-wip save --baseline -m "Before the refactor"

# Edit your files, then save a checkpoint.
go-git-wip save -m "First working version"

# List checkpoints, newest first.
go-git-wip history

# Review a checkpoint using an ID from history.
go-git-wip show CHECKPOINT_ID --patch
```

Saving includes changes to tracked files and new files that Git does not ignore.
It leaves your files and staging decisions intact, including partially staged
files. If the snapshot matches the previous checkpoint, `save` reports `No changes`.

Use `-C /path/to/project` to work in another repository. Add `--json` for scripts.
See the [CLI guide](docs/cli.md) for comparisons, file reads, and checkpoint deletion.

## What gets saved?

By default, `save` captures the whole working tree, excluding ignored files. You
can narrow or expand that selection:

```sh
go-git-wip save src/ README.md  # Only these paths
go-git-wip save --tracked-only # Exclude new, untracked files
go-git-wip save --ignored      # Also include ignored files
```

Each snapshot starts from your current branch commit (`HEAD`). When you save only
selected paths, all other paths use their HEAD content—even if an earlier
checkpoint saved changes to them. Explicit paths must match files eligible for
capture; a typo or an excluded-only selection is an error.

Checkpoints live under `refs/wip/<branch>`. After HEAD changes, for example when
you make a normal commit, the next checkpoint starts a new history from that
commit. Older histories may still be available through `go-git-wip reflog` while
Git retains them.

Checkpoints are temporary local history, not backups. Git's normal expiry and
garbage collection rules apply. The tool can read saved files but does not restore
your working tree. Capture does not support sparse worktrees, unresolved merge
conflicts, or submodules. Included files are not scanned for secrets.

## Use it from Go

The public package is `wip`. It uses the standard library and your local Git
executable; the CLI is optional.

```go
import (
    "context"

    wip "github.com/anzax/go-git-wip"
)

func checkpoint(ctx context.Context, directory string) (wip.CaptureResult, error) {
    repo, err := wip.Open(ctx, directory)
    if err != nil {
        return wip.CaptureResult{}, err
    }
    return repo.Capture(ctx, wip.CaptureOptions{Message: "Work in progress"})
}
```

The [library guide](docs/library.md) covers history, comparisons, errors, and
cancellation. For agent integrations, see [session tracking](docs/agent-sessions.md).

Run a complete example with `make example`. It creates and removes its own temporary
repository, so you can try it without changing an existing project.

## Development

```sh
make check   # Formatting, vet, tests, and the runnable example
make build   # Build bin/go-git-wip
make race    # Optional; requires cgo and a C compiler
```

The root package contains the library. `internal/cli` handles commands and output;
`cmd/go-git-wip` is the executable entry point.

See [requirements](docs/requirements.md), [architecture](docs/architecture.md),
[contributor instructions](AGENTS.md), and [task workflow](tasks/README.md).
Validation details and platform limitations are recorded in the
[library guide](docs/library.md#errors-cancellation-and-limits).

## License

[MIT](LICENSE). The CLI uses Cobra (Apache-2.0), with pflag (BSD-3-Clause) and
mousetrap (Apache-2.0) as indirect dependencies. Dependency licenses remain their own.
