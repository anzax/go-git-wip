# go-git-wip

An MIT-licensed Go library for lightweight working-tree checkpoints stored as native
Git commits. Inspired by the excellent [git-wip](https://github.com/bartman/git-wip)
by Bart Trojanowski, the primary goal is reusable checkpoint operations for other
Go applications. Git operations use the local Git executable.

The library and optional CLI have their own API and behavior. Compatibility with
git-wip is not a goal; command names, options, and storage policies may evolve.

**Status: project bootstrap.** The CLI supports help and version. Checkpoint
operations and the public library API are not implemented yet.

## Development

Requires Go 1.26.2+ and local Git. Bootstrap validation used Go 1.27.2 on Linux/arm64.

```sh
make build
./bin/go-git-wip --help
./bin/go-git-wip --version
make check
```

`make race` additionally runs the race detector when cgo and a C compiler are available.

Cobra v1.10.2 is the only direct dependency; pflag v1.0.10 is indirect. Both were
verified through the Go module proxy's `@latest` on 2026-10-10, with Cobra also
checked against its [release page](https://github.com/spf13/cobra/releases/tag/v1.10.2).
The reusable library will use the standard library and local Git.

The module path and executable name `go-git-wip` are provisional. The executable
has a distinct name from git-wip. Set the final import path before publication;
local consumers can use `go.work` or a module `replace` directive.

## Project map

- Root package `wip`: public library, currently package documentation only.
- `cmd/go-git-wip`: process entry point; `internal/cli`: Cobra adapter.
- [Requirements](docs/requirements.md): checkpoint behavior and validation goals.
- [Architecture](docs/architecture.md): library boundary and Git implementation direction.
- [Tasks](tasks/README.md): work, acceptance criteria, and verification records.
- [Agent instructions](AGENTS.md): development and contribution rules.

## License and inspiration

This project's original material is licensed under [MIT](LICENSE). git-wip is
acknowledged as inspiration, not included as a dependency or implementation.
Dependency licenses remain their own.
