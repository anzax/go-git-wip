# go-git-wip

An MIT-licensed Go library for lightweight working-tree checkpoints stored as native
Git commits. Inspired by the excellent [git-wip](https://github.com/bartman/git-wip)
by Bart Trojanowski, the primary goal is reusable checkpoint operations for other
Go applications. Git operations use the local Git executable.

The library and optional CLI have their own API and behavior. Compatibility with
git-wip is not a goal; command names, options, and storage policies may evolve.

**Status: library implemented and tested.** Capture, history, comparison, file reads,
and ref management are available. The CLI currently supports help and version; the full
command adapter and embedding example follow in the next task slices.

## Development

Requires Go 1.26.2+ and Git 2.30+. Validation uses Go 1.27.2 on Linux/arm64.

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

The module is `github.com/anzax/go-git-wip`; the executable is `go-git-wip`.
Supported platforms are Linux and macOS. Validation runs on Linux only.

## Project map

- Root package `wip`: public checkpoint library.
- `cmd/go-git-wip`: process entry point; `internal/cli`: Cobra adapter.
- [Requirements](docs/requirements.md): checkpoint behavior and validation goals.
- [Architecture](docs/architecture.md): library boundary and Git implementation direction.
- [Tasks](tasks/README.md): work, acceptance criteria, and verification records.
- [Agent instructions](AGENTS.md): development and contribution rules.

## License and inspiration

This project's original material is licensed under [MIT](LICENSE). git-wip is
acknowledged as inspiration, not included as a dependency or implementation.
Dependency licenses remain their own.
