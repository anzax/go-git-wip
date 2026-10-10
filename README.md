# go-git-wip

An MIT-licensed Go library and thin CLI for working-tree checkpoints stored as
native Git commits. Capture preserves HEAD, normal branch refs, the real index,
and working files. Git operations use the local Git executable.

Inspired by the excellent [git-wip](https://github.com/bartman/git-wip) by Bart Trojanowski.
This project has its own API and behavior; compatibility with git-wip is not a goal.

## Quickstart

Requires Go 1.26.2+ and Git 2.30+. Supported platforms are Linux and macOS.
Validation uses Go 1.27.2 and Git 2.56.0 on Linux/arm64; neither macOS nor the
minimum Git version has been execution-tested.

Run the disposable external Go consumer:

```sh
make example
```

It imports `github.com/anzax/go-git-wip`, creates an isolated temporary repository,
captures a baseline and an edit, verifies unchanged capture, compares two history
IDs, checks cancellation, and removes its repository. See the
[consumer source](examples/workflow/main.go) and [library guide](docs/library.md).

Build and use the CLI with your own repository:

```sh
make build
./bin/go-git-wip --help
./bin/go-git-wip -C ./project save --baseline -m "review start"
# Edit files, then capture their working content:
./bin/go-git-wip -C ./project save
./bin/go-git-wip -C ./project history --json
```

The [CLI guide](docs/cli.md) documents commands, JSON, exit codes, and explicit
`--yes` confirmation for deletion.

Install the CLI from this checkout:

```sh
make install
go-git-wip --help
```

Installation uses `go install ./cmd/go-git-wip` and honors `GOBIN`. When `GOBIN`
is unset, Go installs into the first `GOPATH` entry's `bin` directory (normally
`$HOME/go/bin`). Add the installation directory to your `PATH`.

## Capture and history policies

- Tracked working content is captured by default, including staged additions.
  Untracked files require explicit inclusion; ignored files require both untracked
  and ignored inclusion. Selecting a path never overrides these policies.
- Every snapshot starts from HEAD. In partial captures, unselected paths keep HEAD
  content. An unchanged capture succeeds without creating a commit; a baseline
  forces creation.
- Checkpoints live in `refs/wip/<branch>`. A normal HEAD change starts a new stack;
  earlier tips may remain discoverable through retained reflog entries.
- Capture needs an attached local branch and an existing commit. Sparse worktrees,
  unmerged indexes, and gitlinks/submodules are unsupported for capture.

Checkpoints are local and ephemeral, not backups. Reflog expiry and normal Git
garbage collection apply; deleting a ref may remove its reflog. Included files are
not scanned for secrets. The library does not restore files or run garbage collection.

## Development

```sh
make check   # formatting, vet, tests, and the external consumer workflow
make build
make race    # optional; requires cgo and a C compiler
```

Cobra v1.10.2 supplies the CLI; pflag v1.0.10 and mousetrap v1.1.0 are indirect
dependencies. Cobra and mousetrap use Apache-2.0; pflag uses BSD-3-Clause. The public
library uses the standard library and local Git only.

The module is `github.com/anzax/go-git-wip`; the executable is `go-git-wip`.

## Project map

- Root package `wip`: public checkpoint library.
- `cmd/go-git-wip`: process entry point; `internal/cli`: Cobra adapter.
- [Library guide](docs/library.md) and [CLI guide](docs/cli.md): public usage.
- [Requirements](docs/requirements.md): checkpoint behavior and validation goals.
- [Architecture](docs/architecture.md): library boundary and Git implementation direction.
- [Tasks](tasks/README.md): work, acceptance criteria, and verification records.
- [Agent instructions](AGENTS.md): development and contribution rules.

## License and inspiration

This project's original material is licensed under [MIT](LICENSE). git-wip is
acknowledged as inspiration, not included as a dependency or implementation.
Dependency licenses remain their own.
