# Contributing to macfor

Thanks for your interest in contributing. macfor is a macOS forensic artifact
collection tool, and every contribution — code, documentation, artifact
research, bug reports — helps investigators do better work.

## Ways to contribute

- **Report bugs** — open an issue with a reproducible case
- **Propose a new artifact** — open a feature request describing the artifact, where it lives, and why it matters forensically
- **Write a plugin** — implement a new artifact collector (see [Writing a plugin](#writing-a-plugin))
- **Improve docs** — clarify usage, add examples, document discovered edge cases
- **Research** — document a file format, timestamp convention, or version difference

## Development setup

Requirements:

- Go 1.25+
- `golangci-lint` v2 (`brew install golangci-lint`)
- macOS for running the collector against a live system; tests run on any platform

Build and test:

```bash
make build     # produces bin/macfor
make test      # runs all tests with race detector
make lint      # golangci-lint
make all       # lint + test + build
```

## Workflow

1. Fork the repo and create a topic branch (`feat/my-plugin`, `fix/safari-cookies-parse`).
2. Make your change. Keep commits focused and well-described.
3. Run `make all` before pushing.
4. Open a PR against `main`. Describe the problem, the fix, and how you verified it.

All PRs run CI (lint, test, manifest validation, build) on macOS.

## Code style

- Go code is formatted with `gofmt` and `goimports`. CI enforces this.
- Comments must end with a period (`.`). This is enforced by the `godot`
  linter — package docs, functions, structs, inline comments all need it.
- Prefer small, focused functions. Cyclomatic complexity is capped at 15.
- Errors wrap with `%w` and are classified against the sentinels in
  `pkg/macfor` (`ErrNotFound`, `ErrParseError`, `ErrPermissionDenied`, etc.).

## Writing a plugin

Every plugin implements the `Plugin` interface:

```go
type Plugin interface {
    Info() PluginInfo
    Detect(ctx context.Context, src Source) (DetectionResult, error)
    Collect(ctx context.Context, src Source, out Collector) error
}
```

Recommended workflow:

1. **Research the artifact** — file format, location, timestamp convention, version differences. Document findings.
2. **Build test fixtures** — put representative samples in `plugins/<name>/testdata/` (never real user data).
3. **Scaffold the plugin** — `Plugin` struct, `Info()`, `Detect()`, `Collect()`, `artifacts.yaml`.
4. **Implement parsers** — one parser per file/format. Unit-test each one against fixtures.
5. **Integration test** — end-to-end test that exercises `Detect` + `Collect` against fixtures.

See `plugins/shell/` and `plugins/safari/` for reference implementations.

## Forensic soundness

This is a forensic tool. Two rules above all:

1. **Never modify source evidence.** Open files read-only, don't touch timestamps, don't write into user directories.
2. **Fail gracefully and visibly.** If one artifact can't be parsed, log the error and keep going — don't crash the whole collection. Every error ends up in the evidence container.

## Reporting security issues

Please do not open a public issue for security vulnerabilities. See
[SECURITY.md](SECURITY.md) for the disclosure process.

## Code of Conduct

By participating, you agree to abide by our [Code of Conduct](CODE_OF_CONDUCT.md).
