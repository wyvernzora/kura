<div align="center">
    <br>
    <br>
    <img width="256" src="docs/assets/logo-full-256.png">
    <h1 align="center">蔵</h1>
</div>

<p align="center">
<b>kura - anime-first personal library manager</b>
</p>

<hr>
<br>
<br>

**kura** (蔵 — "storehouse") is an anime-first personal library manager for
Plex-style series folders.
It tracks provider metadata, stages incoming media, previews filesystem moves,
and keeps replaced files in per-series trash instead of deleting them.

Kura is not a downloader, request system, notification service, or multi-user
media server. Bring your own inbox/download tooling; Kura organizes the files
you point at it.

## Install

Requirements:

- Go 1.26.3 or newer.
- `mediainfo` on `PATH`, or `KURA_MEDIAINFO_COMMAND` set to its path.
- Docker if you want the container image.
- Lefthook if you contribute changes.

Build the CLI/server binary:

```sh
make build
```

The binary is written to `bin/kura`. `make install` also builds the embedded web
bundle before installing the binary to your Go bin directory.

## Quick Start

Start one Kura server for the library:

```sh
export KURA_LIBRARY_ROOT=/media/anime
export KURA_INBOX_ROOT=/media/inbox
export KURA_TVDB_KEY=...
export KURA_DISABLE_TOKEN=1

bin/kura serve --rest=:8080
```

Then use the CLI from another shell. It talks to the REST server at
`KURA_SERVER_URL` and defaults to `http://127.0.0.1:8080`.

```sh
export KURA_SERVER_URL=http://127.0.0.1:8080
export KURA_DISABLE_TOKEN=1

bin/kura add "Bocchi the Rock!"
bin/kura scan "Bocchi the Rock!"
bin/kura show "Bocchi the Rock!"
```

The normal episode flow is:

```sh
bin/kura stage episode "Bocchi the Rock!" S01E03 'inbox:Bocchi/file.mkv'
bin/kura reconcile plan "Bocchi the Rock!"
bin/kura reconcile apply "Bocchi the Rock!" <token>
```

`stage` records intent only. `reconcile plan` previews moves. `reconcile apply`
moves files and sends displaced media to trash.

## Surfaces

| Surface | Use it for | Docs |
|---|---|---|
| CLI | Human/scripted operations against a running server. | [docs/cli.md](docs/cli.md) |
| REST | Custom UI, automation, and the CLI transport. | [docs/rest-api.md](docs/rest-api.md) |
| MCP | Local agent workflows with no permanent-delete tools. | [docs/mcp.md](docs/mcp.md) |
| Web UI | Browser dashboard served by `kura serve --rest`. | [docs/deployment.md](docs/deployment.md) |

## Deployment

The Docker image runs `kura serve --rest=:8080 --mcp-http=:8081` by default.

```sh
docker build --build-arg VERSION=v0.5.0 -t kura:v0.5.0 .
docker run --rm \
  -e KURA_LIBRARY_ROOT=/library \
  -e KURA_INBOX_ROOT=/inbox \
  -e KURA_TVDB_KEY=... \
  -v /media/anime:/library \
  -v /downloads:/inbox \
  -v kura-token:/var/lib/kura \
  -p 8080:8080 \
  -p 8081:8081 \
  kura:v0.5.0
```

Run one writer per library. Kura targets a single personal library, not
multi-replica writes against the same filesystem. See
[docs/deployment.md](docs/deployment.md) for auth, UID/GID, Kubernetes, and
stuck-claim recovery.

## Documentation

- [Docs index](docs/README.md) — reading order and reference map.
- [Concepts](docs/concepts.md) — vocabulary and invariants.
- [Lifecycle](docs/lifecycle.md) — add, import, scan, stage, reconcile, trash.
- [CLI](docs/cli.md) — commands, selectors, configuration.
- [REST API](docs/rest-api.md) — endpoints, auth, async jobs.
- [MCP](docs/mcp.md) — tools and agent-safety properties.
- [Storage](docs/storage.md) — on-disk JSON/JSONL formats.
- [Changelog](CHANGELOG.md) — release notes.

## Development

```sh
lefthook install
make check
go test ./...
```

Read [AGENTS.md](AGENTS.md) before contributing. It contains the repo-specific
rules for code changes, tests, e2e scenarios, and commit subjects.
