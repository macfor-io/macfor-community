# macfor

**Forensic artifact collection for macOS.**

macfor collects forensic artifacts from live macOS systems and disk images
into a structured, hash-verified evidence container. It's designed for
investigators, incident responders, and security researchers who need
reproducible, tool-verified collections.

Learn more at **[macfor.io](https://macfor.io)**.

[![CI](https://github.com/macfor-io/macfor-community/actions/workflows/ci.yml/badge.svg)](https://github.com/macfor-io/macfor-community/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/macfor-io/macfor-community)](https://goreportcard.com/report/github.com/macfor-io/macfor-community)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/macfor-io/macfor-community)](go.mod)

## Features

- **Live system or disk image** — collect against the running system or a
  mounted image with `--from /path/to/mount`.
- **Structured evidence container** — ZIP containing a manifest, per-file
  SHA-256 hashes, chain-of-custody log, raw artifacts, parsed records, and
  plugin logs.
- **Plugin architecture** — each artifact type is a self-contained plugin
  that implements detection, collection, and parsing.
- **Parsed records** — structured output in JSONL (default) or Parquet for
  direct analysis in DuckDB, Pandas, or pipelines.
- **Rich system context** — every container includes full OS, hardware,
  security posture, and network identity for the source system.
- **Pure Go, no CGO** — single static binary, no runtime dependencies.

## Installation

### Prebuilt binary (macOS)

Download the latest release from the [releases page][releases] and extract:

```bash
tar -xzf macfor_*_darwin_*.tar.gz
xattr -dr com.apple.quarantine ./macfor  # unsigned binary
./macfor --version
```

Binaries are built for darwin/amd64, darwin/arm64, and a universal binary.

### From source

```bash
git clone https://github.com/macfor-io/macfor-community
cd macfor-community
make build
./bin/macfor --version
```

Requires Go 1.25+.

[releases]: https://github.com/macfor-io/macfor-community/releases

### Permissions: Full Disk Access and root

macOS protects much of the data macfor collects, and two separate
permissions apply when collecting from a live system:

- **Full Disk Access.** Without it, macOS blocks reads of protected data,
  such as Safari history, cookies and downloads, even when running as root.
  Grant it in **System Settings → Privacy & Security → Full Disk Access** to
  the app that runs macfor (Terminal, iTerm2, or your IDE), then restart that
  app. macfor warns at startup when Full Disk Access is missing and records
  `full_disk_access` in the manifest's `collection_env`.
- **Root (`sudo`).** Needed to read other users' home directories. Without
  it, macfor collects only what the current user can read.

Anything macfor can't read is recorded as an error in the evidence
container, and the run exits with code `2` (see [Exit codes](#exit-codes)).
Full Disk Access is also recommended when collecting from a mounted image
(`--from`), since macOS can restrict access to files on other volumes.

## Quick start

```bash
# Collect everything from the live system
sudo ./macfor collect --output ./evidence.zip

# Collect a single plugin
./macfor collect --plugin shell.history --output ./evidence.zip

# Collect from a mounted disk image
./macfor collect --from /Volumes/Macintosh\ HD --output ./evidence.zip

# Preview what would be collected (no writes)
./macfor collect --dry-run

# Emit parsed records as Parquet instead of JSONL
./macfor collect --record-format parquet --output ./evidence.zip

# List available plugins
./macfor list plugins
```

### Exit codes

| Code | Meaning |
|------|---------|
| `0` | Collection completed without errors. |
| `1` | Collection failed. |
| `2` | Partial collection: the evidence container was written, but macfor recovered from one or more artifact errors (for example, permission denied without Full Disk Access). The errors are recorded in `manifest.json`, `chain-of-custody.json`, and `logs/`. |

## Plugins

| Plugin | Description | Artifacts |
|--------|-------------|-----------|
| `shell.history` | Shell command history | bash, zsh, fish, sh |
| `browser.safari` | Safari browser state | History, Downloads, Bookmarks, Cookies, Local Storage, Extensions, Session state |

> **Need more coverage?** [macfor Pro](https://macfor.io/pricing) adds 30+
> collectors on the same framework, including Chrome, Firefox, Messages,
> Mail, FSEvents, Unified Logs and keychain metadata.

Missing an artifact you'd like in the community edition? Open a
[feature request][features].

[features]: https://github.com/macfor-io/macfor-community/issues?q=is%3Aissue+label%3Aenhancement

## Evidence container format

```
evidence.zip
├── manifest.json              Collection metadata, system info, plugin results
├── chain-of-custody.json      Access audit log
├── hashes.json                SHA-256 hashes for every file
├── artifacts/                 Raw collected files, laid out by category/plugin/user
│   └── {category}/{plugin_id}/files/{user}/{filename}
├── records/                   Parsed structured data (JSONL or Parquet)
│   └── {plugin_id}/parsed.{jsonl,parquet}
└── logs/                      Per-plugin collection logs
    └── {plugin_id}/collection.log
```

Records are the parsed, typed form of the raw artifacts — ready for SQL
analysis, timeline building, or enrichment.

## Project layout

```
macfor-community/
├── cmd/macfor/              CLI entry point
├── pkg/
│   ├── cli/                 Cobra command tree
│   └── macfor/              Public APIs (Plugin, Source, Collector interfaces)
│       ├── artifacts/       Artifact manifest system
│       ├── sqlite/          Read-only SQLite helpers (WAL-aware)
│       └── parquet/         Parquet schema registry
├── internal/
│   ├── orchestrator/        Plugin coordination
│   ├── container/           Evidence container writer
│   ├── schemas/             Central Parquet schema registration
│   └── sysinfo/             Hardware model database
├── sources/
│   ├── live/                Live macOS system source
│   └── directory/           Directory/disk-image source
└── plugins/
    ├── shell/               shell.history
    └── safari/              browser.safari
```

## Development

```bash
make build               # build ./bin/macfor
make test                # run all tests with race detector
make coverage            # generate coverage.html
make lint                # golangci-lint
make validate-manifests  # verify plugins/*/artifacts.yaml
make all                 # lint + test + build (default target)
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the development workflow,
plugin-authoring guide, and code style.

## Forensic soundness

macfor is built on two non-negotiable rules:

1. **Never modify source evidence.** Files are opened read-only, timestamps
   are preserved, no writes are made into user directories.
2. **Fail visibly.** A parser error in one artifact never stops collection of
   the rest. Every error is logged and recorded in the evidence container.

Evidence containers are hash-verified (SHA-256) and include a chain-of-custody
record suitable for downstream audit.

## Security

Please report vulnerabilities privately — see [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE) © macfor.io contributors
