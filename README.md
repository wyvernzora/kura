# Kura

Kura is an anime-first library manager inspired by tools like Sonarr.

The project is designed around anime as the primary use case. Other series types should work where the model fits, but anime conventions, release patterns, metadata, and automation workflows take priority.

The intended shape is deliberately lean:

- CLI tools for direct manual use.
- MCP tools for agentic workflows.
- Docker-first distribution.
- A UI only if it becomes clearly worth building later.

## Current Status

Kura currently has a small CLI for managing existing Plex-style anime series
directories:

- `kura scan <series>` scans a tracked series directory, records importable
  episode media into `.kura/series.json`, and reports skipped files or ignored
  directories.
- `kura stage --episode <marker> <selector> <path>` admits an explicitly
  selected episode file into the target episode's staged record in
  `.kura/series.json`. Relative paths resolve from the selected series root.
  Use `--replace` when the staged file is intended to replace an active or
  already-staged episode.
- `kura reset --episode <marker> <selector>` removes the staged record for one
  episode without touching the staged file on disk. `kura reset --all <selector>`
  removes every staged record for the series.
- `kura reconcile <dir>` applies Kura's planned filesystem layout, moves staged
  files into the series, and moves replaced active files into `.kura/trash/`.
- `kura reindex` rebuilds `.kura/index.tsv` from per-series metadata.
- `kura meta ...` exposes the current metadata helper commands.

Trash metadata is retained beside each replaced file. Reconcile moves trashed
media under `.kura/trash/<id>/` and writes the corresponding
`.kura/trash/<id>/meta.json`.

The normal local flow is:

```sh
kura scan <series-dir>
kura stage --episode S01E03 <selector> --replace /media/anime/inbox/example.mkv
kura reset --episode S01E03 <selector>
kura reconcile <series-dir>
```

Use `kura import` or `kura add` to create the tracked `series.json` spine before
scanning.

## Requirements

- Go 1.26.2 or newer.
- Docker, if building or running the container image.

## Run Locally

```sh
go run ./cmd/kura
```

Useful environment:

- `KURA_LIBRARY_ROOT`: root directory containing series directories.
- `KURA_MEDIAINFO_COMMAND`: path to the `mediainfo` executable, when it is not
  on `PATH`.
- `KURA_TVDB_KEY`: API key for TVDB metadata lookups.
- `KURA_PREFERRED_LANGUAGES`: comma-separated preferred metadata languages.

## Build

```sh
go build -o bin/kura ./cmd/kura
```

## Development Checks

Run the same core checks used during local development:

```sh
make check
```

`make check` runs `gofmt`, `go vet`, `gopls check`, `go test`, and a local binary build. `gopls check` surfaces editor diagnostics such as Go's `modernize` analyzer warnings.

## Docker

Build the image:

```sh
docker build -t kura .
```

Run it:

```sh
docker run --rm kura
```
