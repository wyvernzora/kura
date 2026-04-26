# Agent Notes

These notes capture project intent and working conventions for future agent threads.

## Project

- Name: Kura.
- Domain: anime-first library manager, broadly similar in category to Sonarr.
- Priority: anime behavior comes first; other series types can work when compatible but should not drive the design.
- Product shape: no bloat. Prefer CLI tools for manual use and MCP tools for agentic use.
- UI: possible in the distant future, but not a current priority.
- Distribution: Go application shipped as a Docker container.

## Engineering Conventions

- Language: Go.
- Main command entrypoint: `cmd/kura`.
- Keep dependencies intentional and minimal.
- Prefer clear CLI/MCP surfaces over background magic.
- Preserve a small, automation-friendly core before adding optional layers.

## Useful Commands

```sh
go run ./cmd/kura
go test ./...
go build -o bin/kura ./cmd/kura
docker build -t kura .
docker run --rm kura
```
