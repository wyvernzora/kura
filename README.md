# Kura

Kura is an anime-first library manager inspired by tools like Sonarr.

The project is designed around anime as the primary use case. Other series types should work where the model fits, but anime conventions, release patterns, metadata, and automation workflows take priority.

The intended shape is deliberately lean:

- CLI tools for direct manual use.
- MCP tools for agentic workflows.
- Docker-first distribution.
- A UI only if it becomes clearly worth building later.

## Current Status

Kura is at the initial scaffold stage. The current executable prints:

```text
Hello, World!
```

## Requirements

- Go 1.26.2 or newer.
- Docker, if building or running the container image.

## Run Locally

```sh
go run ./cmd/kura
```

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
