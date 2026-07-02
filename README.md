# Kura

Anime-first library manager. Keeps your shows tidy, consistently
named, and matched against an online metadata source so Plex /
Jellyfin pick them up cleanly — without you renaming files by hand.

## What Kura is

Kura watches a folder of anime. You drop new episode files in, tell
Kura which episode they are, preview the moves, and apply. Episodes
get renamed to a canonical layout. Files that get replaced go to a
per-show trash bin instead of being deleted.

What makes it different: anime conventions drive the design (Sonarr-
style tools handle anime awkwardly); the same workflows are reachable
from a CLI, a REST API, and an MCP tool surface, so it works equally
well for a human at a terminal, a script, or an AI agent; nothing
happens to your files until you ask.

## Tenets

- **Anime first, other series usable.** Anime release conventions
  drive the design. Non-anime works where the model fits, but isn't
  the focus.
- **Agent first, human usable.** Designed to be driven by an AI
  agent (MCP) or a script (CLI / REST). The bundled web UI is a
  browse-and-dashboard experience for humans; anything that mutates
  state still goes through CLI, MCP, or REST.
- **Only manages your library.** No downloading, no torrent or
  Usenet integration, no notifications, no calendar, no requests
  system. Bring your own everything-else; Kura organizes what's in
  your library or what you point at it.
- **Self-hosted, single-tenant.** One process per library, runs on
  your hardware. Not a SaaS, not multi-user — homelab-shaped.
- **Nothing happens without your say-so.** Destructive operations
  are explicit, preview-before-apply, and recoverable from trash.
  You won't wake up to renamed files you didn't ask for.
- **Open files, no lock-in.** Your library is a normal folder of
  normal files. Kura's bookkeeping is plain JSON next to each show
  (`<series>/.kura/`) — rename a show's directory or move the whole
  library to another disk and the metadata travels with it. You can
  read it, edit it, or walk away from Kura without exporting
  anything.

## How it works

A typical journey, in plain terms:

1. **Tell Kura about a show.** Kura looks it up online and remembers
   the season / episode list.
2. **Point Kura at the folder.** Kura reads what's already there
   and matches each file to an episode.
3. **Add a new file.** When you download a new episode, you tell
   Kura "this file is episode X." Nothing has moved yet — Kura is
   just making notes.
4. **Preview the changes.** Kura shows you what it would rename and
   where it would put each file.
5. **Apply.** Kura performs the moves. Files being replaced go into
   a per-show trash folder so nothing is lost.
6. **Clean up later.** Once you're sure the new files work, you can
   empty the trash. If something went wrong, you can restore from
   it.

For the engineer-facing version with edge cases, see
[docs/lifecycle.md](docs/lifecycle.md).

## Glossary

The words you'll actually see — the same vocabulary a future web UI
would expose:

- **Library** — the folder where all your shows live.
- **Series** — one show. A subfolder of the library.
- **Episode** — one slot in a show's episode list. Each episode
  can have a file attached.
- **Staged file** — a file you've told Kura about that hasn't been
  moved into place yet. Reversible until you apply.
- **Trash** — a per-show holding area for files Kura replaced. They
  stay there until you empty them; you can restore from it.
- **Metadata source** — the online service Kura uses to look shows
  up. Currently TVDB.

For the engineer-facing terms (MetadataRef, SeriesRef, EpisodeRef,
spine, claim, mutator, CAS, ULID, on-disk JSON layout), see
[docs/concepts.md](docs/concepts.md).

## Quick start

Requirements:

- Go 1.26.3 or newer.
- `mediainfo` on `PATH` (or set `KURA_MEDIAINFO_COMMAND`).
- Docker, if building or running the container image.

Build and install:

```sh
make build      # builds bin/kura
make install    # rebuilds and installs to $(go env GOBIN) or $GOPATH/bin
```

Set the library root (and a TVDB key if you want metadata workflows):

```sh
export KURA_LIBRARY_ROOT=/media/anime
export KURA_TVDB_KEY=...
```

Three-line walkthrough:

```sh
kura add "Bocchi the Rock!"            # register a new series
kura scan "Bocchi the Rock!"           # adopt existing files
kura show "Bocchi the Rock!"           # inspect state
```

For the full CLI, REST, and MCP surfaces, see the table below.

## Surfaces

| Surface | When to use it | Reference |
|---|---|---|
| Web UI  | Browse and inspect the library from a browser. Served at the REST port (default `:8080`). Read-only / dashboard — mutations go through CLI, MCP, or REST. | embedded in the binary |
| CLI     | Manual or scripted use from a shell. | [docs/cli.md](docs/cli.md) |
| REST    | A custom UI or a remote agent. | [docs/rest-api.md](docs/rest-api.md) |
| MCP     | A local AI agent. | [docs/mcp.md](docs/mcp.md) |

## Deployment

Alpine-based Docker image, single-writer per library, bearer-token
auth as a deploy gate (auto-generated and persisted at
`/var/lib/kura/token` on first start; bypass with
`KURA_DISABLE_TOKEN=1` when fronting with an authenticating proxy).

```sh
docker build --build-arg VERSION=v0.2.0 -t kura:v0.2.0 .
docker run --rm \
  -e KURA_LIBRARY_ROOT=/library \
  -e KURA_INBOX_ROOT=/inbox \
  -v /media/anime:/library \
  -v /downloads:/inbox \
  -v kura-token:/var/lib/kura \
  -p 8080:8080 \
  kura:v0.2.0 serve --rest=:8080
```

For Kubernetes manifests, NFS / UID matching, and the stuck-claim
recovery rules, see [docs/deployment.md](docs/deployment.md).

## Documentation

Index of the engineer-facing docs lives in
[docs/README.md](docs/README.md):

- [Concepts](docs/concepts.md) — vocabulary, domain model,
  invariants.
- [Lifecycle](docs/lifecycle.md) — every workflow with edge cases.
- [CLI](docs/cli.md) — every `kura <verb>`.
- [REST API](docs/rest-api.md) — endpoint catalog, auth, jobs.
- [MCP](docs/mcp.md) — tools and agent-safety properties.
- [Deployment](docs/deployment.md) — Docker, Kubernetes,
  single-writer.
- [Storage](docs/storage.md) — on-disk file formats.
- [Changelog](CHANGELOG.md) — release notes.

## Contributing

[AGENTS.md](AGENTS.md) is the operating manual for both human and
coding-agent contributors. Read it before opening a PR.

```sh
make check      # gofmt + vet + gopls + tests + binary build
```
