# Agent Notes

These notes capture project intent and working conventions for future agent threads.

## Project

- Name: Kura.
- Domain: anime-first library manager, broadly similar in category to Sonarr.
- Priority: anime behavior comes first; other series types can work when compatible but should not drive the design.
- Product shape: no bloat. Prefer CLI tools for manual use and MCP tools for agentic use.
- UI: possible in the distant future, but not a current priority.
- Distribution: Go application shipped as a Docker container.

## Library Layout

- `docs/SCRATCH.private.md` is the ignored coding-agent context/spec scratchpad. Actual local paths and observed machine-specific details live in ignored `docs/LOCAL.private.md`.
- Use generic public examples such as `/media/anime/series` and `/media/anime/inbox`; keep personal mount paths in ignored local notes.
- Kura targets existing Plex-style anime series libraries and should preserve their structure during bootstrap.
- The inbox may be a general download directory with non-anime files, so Kura should only act on explicitly referenced files.
- Per-series Kura metadata lives under `<series>/.kura/series.json`; do not use bare `.series.json`.
- Per-series `.kura/` is for metadata/control files only, not media.
- Regular seasons live under `Season <N>/`.
- Season 0 specials should be treated as root-level series files in the target layout, while legacy `Season 0/` folders may exist and must be tolerated during bootstrap.
- BD/DVD extras use `Season <N>/Extra/` with no required internal structure.
- Preferred target episode naming convention: `<title> - S02E03 (WebRip HEVC 1920x1080).mkv`.
- Filesystem title selection uses ordered `KURA_PREFERRED_LANGUAGES`; empty or unset means
  provider canonical title, and missing translations always fall back to canonical.

## Engineering Conventions

- Language: Go.
- Go version: 1.26.2 or newer.
- Main command entrypoint: `cmd/kura`.
- All Kura-generated JSON files must include top-level `schemaVersion`; initial version is `1`.
- Series metadata uses local `id`, provider-neutral `providerRefs`, and `preferredProvider`; do not add provider-specific top-level ID fields.
- Keep dependencies intentional and minimal.
- Always prefer established libraries for common tasks such as language tags, time/date parsing, structured data parsing, CLI handling, hashing, and media/container metadata instead of rolling custom implementations.
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
