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
- Kura does not currently scan a central inbox. `kura stage` accepts explicitly referenced absolute media paths, which may come from any inbox or download directory.
- Per-series Kura metadata lives under `<series>/.kura/series.json`; do not use bare `.series.json`.
- Staged external media entries live under `<series>/.kura/staged.json`.
- Trash inventory lives under `<series>/.kura/trash.json`; trashed media lives under `<series>/.kura/trash/<trash_id>/`.
- Active tracked media must not live under `.kura/`. Kura-managed trash media is the explicit exception.
- Regular seasons live under `Season <N>/`.
- Season 0 specials should be treated as root-level series files in the target layout, while legacy `Season 0/` folders may exist and must be tolerated during bootstrap.
- BD/DVD extras use `Season <N>/Extra/` with no required internal structure; current sync reports these directories as skipped and does not manage their contents.
- Preferred target episode naming convention: `<title> - S02E03 (WebRip 1080p).mkv`.
- Generated media filenames use the current series directory name as `<title>`; `series.json` does not store a `filesystemTitle`.
- Resolution should render as a known shorthand such as `4K`, `1440p`, `1080p`, `720p`, or `480p` when possible, with raw resolution fallback.
- Source should remain in generated filenames because mediainfo cannot reconstruct it reliably. Codec is intentionally not included in generated filenames right now.

## Current Workflows

- `kura sync <dir>` scans a series directory, initializes metadata when needed, records recognized episode media, refreshes changed facts for same-path episodes, and reports skipped files/directories.
- `kura sync --replace <dir>` is required when a discovered file replaces an existing active season/episode with a different media path; replaced active records move to trash metadata.
- `kura stage <dir> [opts] <absolute-media-path>` records an explicit external media file in `.kura/staged.json`; active or staged season/episode collisions require `--replace`.
- `kura reconcile <dir>` moves staged files into the active layout, moves replaced active files under `.kura/trash/<trash_id>/`, updates metadata, and removes empty staged metadata.
- `kura reconcile` does not rename the series root. It uses the current directory name for generated media filenames.
- If sync or reconcile has no changes, the CLI must not ask to apply anything.
- Current series selectors resolve direct child directories below `KURA_LIBRARY_ROOT`; fuzzy selectors and provider/local-id selectors require future library-wide indexing.

## Engineering Conventions

- Language: Go.
- Go version: 1.26.2 or newer.
- Main command entrypoint: `cmd/kura`.
- All Kura-generated JSON files must include top-level `schemaVersion`; initial version is `1`.
- Series metadata uses local `id`, provider-neutral `providerRefs`, and `preferredProvider`; do not add provider-specific top-level ID fields.
- Keep persistent models dumb. Workflow behavior belongs in `internal/ops`.
- Keep dependencies intentional and minimal.
- Always prefer established libraries for common tasks such as language tags, time/date parsing, structured data parsing, CLI handling, hashing, and media/container metadata instead of rolling custom implementations.
- Prefer clear CLI/MCP surfaces over background magic.
- Preserve a small, automation-friendly core before adding optional layers.
- `KURA_TVDB_KEY` is the TVDB API environment variable currently used by the code.

## Useful Commands

```sh
go run ./cmd/kura
go test ./...
go build -o bin/kura ./cmd/kura
make check
docker build -t kura .
docker run --rm kura
```
