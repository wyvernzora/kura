# AGENTS.md — kura monorepo

The single agent-context file for the whole repo. Read the user-global
rules first (`~/.agents/AGENTS.md`, `~/.agents/go.md`); they apply unless
explicitly contradicted here. Per-service context, learnings, and
overrides live in the service sections below. Paths inside a service
section are relative to that service's directory unless they start with
`services/`.

## Layout

| Path | What it is |
|---|---|
| `services/library/` | kura core: library manager, REST + MCP API (no embedded UI) |
| `services/releases/` | release indexer + `sources/{dmhy,nyaa}` crawler modules |
| `services/webui/` | suite web UI: static SPA + Caddy proxy to the service APIs |
| `integrations/n8n/{kura,releases}/` | the two n8n node packages |
| `prompts/` | reserved: versioned agent/matcher prompts |
| `deploy/` | reserved: deployment manifests |

One `go.work` spans all four Go modules. `make check` at the root fans
out to per-service Makefiles; prefer per-service `make` during iteration.

Tooling config is repo-wide and lives at the root: `.golangci.yml`,
`.gitignore`, `.gitattributes`, `.editorconfig`, `.tool-versions`,
`lefthook.yml`. Do not add per-service copies — prefer unifying
conventions repo-wide in general. Two exceptions: `.dockerignore` stays
per-service (each service is its own docker build context), and
Makefiles stay fully self-contained per service — no shared make
fragments unless the whole Makefile system moves to the root someday.

`scratch/` at the repo root is the gitignored agent scratch directory —
coding-agent context, specs, plans, and local notes. Read it before
starting non-trivial work; update it when context shifts.
`scratch/local.md` holds actual mount paths and machine-specific details;
committed docs use generic public examples (`/media/anime/series`,
`/media/anime/inbox`).

## Commit convention (enforced)

Conventional Commits v1.0.0, subject ≤72 chars, enforced by
`.githooks/check-commit-subject.sh` via lefthook and CI:

- Types: feat, fix, docs, refactor, test, build, ci, chore, perf, revert.
- Scopes (closed enum): `library`, `indexer`, `backup`, `webui`, `repo`,
  `deps`, `release`, `n8n`, `deploy`. Scope says where; type says what
  kind. Adding a scope is a deliberate one-line change to the hook.
- Merge commits and `fixup!`/`squash!` markers are exempt.
- Commit messages must not mention Claude/AI tooling —
  `.githooks/block-ai-brand-commit-msg.sh` rejects them. No
  `Co-authored-by` / "Generated with …" trailers.
- Hooks install via `lefthook install` (root `lefthook.yml`); run once
  per checkout.

## Versioning and artifacts

- Repo-wide versioning: one `vX.Y.Z` tag line for the whole monorepo,
  continuing the original kura lineage. The release workflow builds every
  service image at that version.
- Directories and commit scopes are unprefixed; binaries and images keep
  the `kura-` prefix (`kura-releases`, `ghcr.io/wyvernzora/kura-server`,
  …) because they leave the repo's namespace.

## End-to-end tests (all services)

- E2E tests, once authored, are load-bearing regression contracts. ANY
  change to an existing e2e scenario — whether the work is related or
  unrelated, whether the change is one assertion or a rewrite, whether or
  not a feature change "requires" the update — requires explicit user
  approval before the edit. The agent must surface the proposed change
  with a detailed explanation of (a) what behavior the scenario currently
  asserts, (b) what is changing in the scenario, and (c) why the system
  change demands the scenario change rather than a system fix. No
  exceptions. If a scenario fails because the system under test changed,
  surface the failure for review; do not mutate the assertions to turn
  red into green. This covers the library e2e suite and the releases
  conformance/smoke suites alike.
- When the user explicitly reports that the app is not behaving as
  required (a bug report, "this is wrong," "shouldn't do X"), add an e2e
  regression scenario alongside the fix when feasible. The scenario
  reproduces the reported behavior in its red state and asserts the
  corrected behavior in its green state. Skip only when the bug genuinely
  cannot be exercised through the e2e harness; surface that limitation in
  the response so the user can decide whether to add coverage some other
  way.

---

## services/library — kura core

### About Kura

- **Name:** Kura.
- **Domain:** anime-first library manager, broadly similar in category to Sonarr.
- **Priority:** anime behavior comes first; other series types can work when compatible but should not drive the design.
- **Product shape:** no bloat. Prefer CLI tools for manual use and MCP tools for agentic use.
- **Operational scale:** personal anime library automation, not a high-throughput multi-writer file transaction system. Expect new episodes a few times a week and occasional season upgrades, usually flowing qbit/download inbox -> Kura -> library with an LLM agent driving Kura. Kura is the only intended writer inside the library root; other tools should be readonly there, and direct human writes are rare. Avoid engineering for AWS-S3-scale concurrency or hostile library writers unless the product requirements explicitly change.
- **UI:** none embedded — the suite web UI lives in `services/webui` (SPA + Caddy proxy fronting this service's REST API). `kura serve --rest` serves the API only.
- **Distribution:** Go application shipped as a Docker container.

### Stack

- **Language:** Go (1.26.3 or newer; the codebase uses the generic `errors.AsType` from the 1.26 stdlib). Pinned in `go.mod`, `.tool-versions`, and `Dockerfile`.
- **Main command entrypoint:** `cmd/kura`.
- **Workflow facade:** `internal/workflow` exposes Add/Import/Show/List/Stage/Scan/Reset/Trash/Reindex/Remove + Reconcile{Plan,Apply,Recover}. The public Go API for CLI and MCP transports.
- **Reconcile internals:** `internal/reconcile` builds plans, applies them, and recovers stuck claims. Imported only via the workflow shim.
- **Scan internals:** `internal/scan` walks a series directory, parses filenames, and reconciles findings against persisted state. Imported only via the workflow shim.
- **Resolution:** `internal/resolve` matches user-supplied selectors to a series ref via registered strategies (text, `tvdb:<id>`, `dir:`, etc.).
- **Storage primitives:** `internal/storage/{indexfile,seriesfile,planfile,paths,seriesdir,trashfile}` own the on-disk JSON / JSONL formats and CAS write semantics. One package per file kind. Sibling storage packages do not import each other; `paths` is the leaf.
- **Coordination:** `internal/coord` provides per-series and per-index ctx-cancellable serialization plus CAS retry. Owns `Holder` and `Mutator` types.
- **Jobs:** `internal/jobs` runs async workflow ops and exposes a registry for polling-based clients (MCP).
- **Provider:** `internal/provider` is the metadata-provider abstraction; `internal/provider/tvdb` is the only implementation today.
- **Domain types:** `internal/domain/{refs,media,series,filename,selector}` are pure types shared across packages. Leaf-level — they do not import sibling internal packages.
- **Inbox:** `internal/inbox` walks the `KURA_INBOX_ROOT` tree on demand (NFC-normalized entries; dotfiles and download-in-flight markers like `.partial`/`.!qB` hidden by default). Selector primitives live in `internal/domain/selector`.
- **Search keys:** `internal/searchkey` computes the per-series fuzzy-search blob shipped on `ListRow` — flattened, deduplicated alias lines fed to client-side fuse.js. Never user-facing.
- **Transports:** `kura serve` hosts the servers: `internal/server/mcp` (MCP tool surface, stdio or streamable HTTP), `internal/server/rest` (REST API under `/api/*`, bearer-token auth via `internal/server/auth`). Servers depend on `internal/workflow` for behavior. `cmd/kura` hosts the CLI; its verbs are REST clients via `internal/cli/client` (discovery through `KURA_SERVER_URL`, default `http://127.0.0.1:8080`) — only the `serve` command imports `internal/workflow` in-process.
- **Cross-cutting:** `internal/progress` (ctx-routed reporter), `internal/textnorm` (NFC), `internal/fsop` (atomic filesystem moves), `internal/mediainfo` (mediainfo binding), `internal/config` (env loading), `internal/errkind` (typed error categorization), `internal/sweep` (periodic background work), `internal/response` (wire response shapes), `internal/cli/*` (CLI rendering / stdio / REST client).
- **Container:** Docker, single-binary image.

### Commands

```sh
go run ./cmd/kura          # run the CLI from source
go test ./...              # full test suite
go build -o bin/kura ./cmd/kura
make check                 # lint + vet + tests (preferred verification)
docker build -t kura .
docker run --rm kura
```

Prefer single-package or single-test runs during iteration (`go test ./internal/workflow/...`, `go test -run TestX ./...`). Full suite is for the final verification pass.

### Library layout (on disk)

- Kura targets existing Plex-style anime series libraries and preserves their structure during bootstrap.
- Library index: `<library>/.kura/index.jsonl` (never inside a series directory).
- Per-series Kura metadata: `<series>/.kura/series.json` (never bare `.series.json`).
- Staged external media entries live inside `series.json` episode records, not in `staged.json`.
- Trash metadata lives beside trashed media at `<series>/.kura/trash/<trash_id>/meta.json`, not in `trash.json`.
- Active tracked media must not live under `.kura/`. Kura-managed trash media is the explicit exception.
- Regular seasons: `Season <N>/`.
- Season 0 specials are treated as root-level series files in the target layout. Legacy `Season 0/` folders may exist and must be tolerated during bootstrap.
- BD/DVD extras: `Season <N>/Extra/`, no required internal structure. Scan reports these as skipped and does not manage their contents.
- Target episode naming: `<title> - S02E03 (WebRip 1080p).mkv`.
- Generated filenames use the current series directory name as `<title>`. `series.json` does not store a `filesystemTitle`.
- Resolution shorthand when possible (`4K`, `1440p`, `1080p`, `720p`, `480p`); raw resolution is the fallback.
- Source stays in generated filenames (mediainfo cannot reconstruct it). Codec is intentionally omitted from generated filenames right now.

### Repo conventions

- All Kura-generated JSON files include top-level `schemaVersion`. Initial version is `1`.
- Series metadata uses a single source-neutral `metadataRef`. Do not add local series IDs, `providerRefs`, or `preferredProvider`.
- Keep dependencies intentional and minimal.
- Prefer clear CLI/MCP surfaces over background magic.
- Preserve a small, automation-friendly core before adding optional layers.
- `KURA_TVDB_KEY` is the TVDB API environment variable currently used by the code.
- `KURA_LIBRARY_ROOT` scopes series selectors. Metadata-ref selectors use `<library>/.kura/index.jsonl`; run `kura reindex` to rebuild it from per-series metadata.
- `KURA_INBOX_ROOT` is the download-inbox root that `inbox:<rel>` selectors resolve against. Required at `kura serve` startup (must exist, be a directory, and be disjoint from the library root); CLI verbs reach the inbox only through the server's REST surface.
- `KURA_HOST_ID` overrides `os.Hostname()` for the identity Kura stamps into claim holders and CAS mutators. Set this in container deployments to a stable value (e.g. the underlying host's actual hostname) so a previous container's stuck claim is detected as same-host on restart and can be auto-broken; without it, every container restart mid-apply requires a manual `kura reconcile recover`.
- `KURA_AIRING_TAIL_DAYS` controls how many days after a cour's last episode airs it still counts as airing. Default `7`; `0` disables the tail. Integer days; empty / invalid / negative values fall back to the default.
- `KURA_LOG_RETENTION_DAYS` sets how long the periodic sweep retains forensic JSONL logs — reconcile plan logs at `<series>/.kura/reconcile/*.jsonl` and per-job history logs at `<library>/.kura/jobs/<ulid>.jsonl`. Default `7`. Integer days; empty / invalid / negative values fall back to the default.

### Current workflows

- `kura scan <series>` — scan a tracked series directory, record recognized episode media into `series.json`, refresh changed facts for same-path episodes, keep empty spine episodes, and report skipped files/directories.
- `kura scan --replace <series>` — required when a discovered file replaces an existing active season/episode at a different media path.
- `kura stage episode <selector terms> <SxxEyy> <inbox:media-selector>` (plus `stage trash`, `stage extra`) — record an explicit external media file inside the target episode's `series.json` staged record. Media is an `inbox:<rel>` selector under `KURA_INBOX_ROOT` (or `series:<rel>` for in-place metadata override); absolute paths are not accepted. Active or staged season/episode collisions require `--replace`.
- `kura reconcile plan <series>` — resolve the series selector through the library index, write a JSONL plan under `<series>/.kura/reconcile/<token>.jsonl`, and print the token. Token = snapshot hash; apply re-validates the snapshot at execute time. Empty plans write no plan file.
- `kura reconcile apply <series> <token>` — apply a saved reconcile plan, move staged files into the active layout, move replaced active files under `.kura/trash/<trash_id>/`, write per-trash `meta.json`, append move/result records to the plan JSONL file, and update `series.json`. Does not rename the series root; uses the current directory name for generated media filenames.
- If scan or reconcile plan has no changes, the CLI must not ask to apply anything.
- Kura does not auto-scan or auto-import the inbox. `kura inbox list [path]` lists entries under `KURA_INBOX_ROOT` on demand (recursive up to depth 5, kind/glob filters, hidden files omitted unless `--all`; accepts an exact file path); `kura stage` consumes `inbox:<rel>` selectors from it.

### Forbidden

- Active tracked media under `.kura/` (only Kura-managed trash is allowed there).
- Bare `.series.json` outside `.kura/`.
- Adding `providerRefs`, local series IDs, or `preferredProvider` to series metadata.
- Background magic in lieu of explicit CLI/MCP surfaces.

### Learnings

**Accumulated corrections. This section is for the agent to maintain, not just the human.**

When the user corrects your approach, append a one-line rule here before ending the session. Write it concretely ("Always use X for Y"), never abstractly ("be careful with Y"). If an existing line already covers the correction, tighten it instead of adding a new one. Remove lines when the underlying issue goes away (model upgrades, refactors, process changes).

- For `kura import`, prepend dirname only for empty/text extra terms, preserve `tvdb:<id>` as authoritative, pass mixed text/`tvdb:` terms to the resolver, and treat `dir:` terms as text unless a strategy claims that prefix.
- Attach CLI progress reporting through `context.Context` using `internal/progress` so nested workflows like implicit `reindex` can report.
- Organize commits as appropriate for the work; split unrelated or review-distinct changes into separate commits.
- Keep selector `Term` string-like; strategies own any prefix or shape parsing they need. In resolver strategy matching, treat prefixed terms as text unless a registered strategy claims the prefix; authoritative strategies like metadata ID stop later strategy matching.
- Persist series-level `preferredTitle` and `canonicalTitle` in `series.json`; keep episode-level provider data limited to slot identity and air date.
- For download release triage, prefer staging candidate files with Kura and using the staging report for media facts; reset staged records when the release is not recommended.
- For series-level actions that need review before mutation, use explicit selector-based `plan` and `apply <selector> <token>` workflows instead of combined dry-run/yes commands.
- Top-level `internal` packages must not import child packages of sibling top-level packages; import the sibling facade instead, while child packages may import siblings under their own top-level package.
- When extracting an implementation subpackage, move the full cohesive workflow or leave it in place; do not leave runner/helper remnants in the facade package unless they are intentional public API.
- Workflow responses must never carry raw filesystem paths. Every path field is a scheme-tagged selector: `series:<rel>` for files inside a series root, `inbox:<rel>` for files under the inbox root, `library:<rel>` for paths under `KURA_LIBRARY_ROOT` (e.g. `Show.Root` emits as `library:<series-dir>`). Use the `seriesSelector` / `inboxSelector` / `librarySelector` helpers in `internal/workflow/paths.go` at the response-construction boundary; they panic on outside-root paths so contract violations fail loudly. CLI table renderers strip the prefix for human display via `stripPathScheme`; JSON / MCP / REST keep the prefix.
- Treat Kura's library as a single-writer personal anime library on low-IOPS storage, often NFS-backed; do not add high-throughput, multi-writer, or cloud-object-store durability machinery unless a real Kura workflow requires it.
- Thread deploy-time row-building policy from `cmd/kura` through workflow/index builders; do not read env vars directly from storage packages.
- Kura does not guarantee forward compatibility for old binaries reading newer on-disk metadata; prefer a clear current-schema contract over version machinery whose only benefit is rollback ergonomics.
- Use namespaced workflow-tag conventions in the UI and integration guidance: `priority:high`, `priority:low`, `maintenance:requested`, and `maintenance:disabled`.
- Commit subjects use Conventional Commits with the closed scope enum above; the former `<scope>: <message>` convention is retired (2026-07, monorepo migration).
- Run LTO/LTFS tooling on a dedicated VM with its own read-only `KURA_LIBRARY_ROOT` mount and VM-local disposable tape state; derive hot/cold placement from payload-change history rather than raw metadata-write frequency.
- Treat LTO as operator-assisted homelab archival for valuable-but-replaceable data; prefer visible manual recovery conflicts and operator-approved risk over high-availability protocols or automatic conflict resolution.

---

## services/releases — release indexer

**The canonical reference is [`services/releases/docs/design.md`](services/releases/docs/design.md)** —
the load-bearing design (identity model, data model, queue semantics,
external contracts, seams). Its §2 invariants are settled; do not
relitigate them. For deployment and ops see
[`services/releases/docs/operations.md`](services/releases/docs/operations.md);
for the spec→test map see
[`services/releases/docs/conformance-matrix.md`](services/releases/docs/conformance-matrix.md).
Read design.md before any sizable change.

### About the release indexer

- **Name:** kura-releases (was the standalone takuhai repo, 宅配 — "home delivery / courier").
- **Domain:** anime **release index** — a dumb, durable store + work queue + query
  API. It records what an external matching agent reports; it holds no matching policy.
- **Ingestion is external push.** n8n drives stateless crawlers (`POST /crawl`) and
  pushes posts to the indexer (`POST /ingest`). The indexer holds no cursor, runs no
  scheduler, makes no outbound calls.
- **Surfaces:**
  - REST (n8n-driven): `POST /ingest`, `POST /queue/claim`, `GET /queue/stats`,
    `POST /submit`, `GET /magnets/{infohash}`, and `GET /releases/{infohash}`.
  - MCP (consumer-only): `list_releases`, `get_release`, `resolve_magnets`, over streamable HTTP at `/mcp`.
  - `/healthz` — a live DB ping.
- **Transport:** HTTP (`--addr=:8080`).
- **Distribution:** Go binary + Docker container. Crawlers are separate binaries
  (`sources/dmhy/cmd/kura-releases-dmhy`, `sources/nyaa/cmd/kura-releases-nyaa`).

### Invariants (do not violate — see design.md §2)

- The indexer is the store; the matcher is the brain. No thresholds/heuristics in the
  indexer.
- A release *is* its infohash (canonical = 40-hex v1 btih; base32 decoded first; pure
  v2 skipped).
- Raw is immutable and kept forever; the match is a derived, recomputable layer.
- Canonical refs are **opaque** strings — shape-validated only, never checked against
  TVDB/kura. The indexer never calls kura.
- Everything is idempotent: re-ingest, re-claim after a crash, re-submit must all be
  safe (a disposition fences on `claim_token`).
- Sources are pluggable: a new source is a new crawler speaking the same `RawPost`
  shape — it must not touch the indexer's core.

### Stack

- **Language:** Go 1.26.3+ (pinned in `go.mod` / `.tool-versions`). Three Go modules
  live here — the service module plus the `sources/{dmhy,nyaa}` crawler modules — all
  members of the monorepo-root `go.work`. CI builds each module with `GOWORK=off`.
- **Entry point:** `cmd/kura-releases/main.go` — flag-driven, env fallbacks (prefix
  `KURA_RELEASES_`). Runs migrations at startup, then serves HTTP.
- **Store:** PostgreSQL via `pgx`. Migrations are **embedded goose** in
  `db/migrations/`, run at startup under an advisory lock.
- **MCP SDK:** `github.com/modelcontextprotocol/go-sdk` (streamable HTTP at `/mcp`).
- **Logs:** structured `slog` (JSON) to stderr, `--log-level`.

### Package map

```
cmd/kura-releases/   flag-driven entrypoint: wiring and lifecycle (migrate → serve → drain)
pkg/rawpost/         shared wire contract: RawPost + IngestSummary (a leaf)
internal/config/     Config struct; flag + env binding; validation
internal/infohash/   NormalizeInfohash + ErrSkipInfohash — the dedup key
internal/cursor/     list_releases cursor encode/decode + ref/path binding + ref validation
internal/dispatch/   transport-neutral worker/consumer dispatch + sentinel→code helper
internal/rest/       REST /ingest, /queue/*, and /submit handlers
internal/store/      Store interface + param/result types + sentinel errors
internal/store/postgres/  pgx implementation (only backend in v1)
internal/mcp/        MCP server: consumer tools only; HTTP /mcp; calls dispatch
internal/health/     /healthz: DB ping via the Ping seam
db/migrations/       embedded goose SQL migrations
sources/dmhy/        stateless DMHY crawler module (own go.mod): RSS+HTML parsers + POST /crawl
sources/nyaa/        stateless Nyaa crawler module (own go.mod): same /crawl contract
```

`store` is a leaf (imports neither `rawpost` nor the REST layer); `mcp` reaches store
only through `dispatch`; REST queue/submit routes use `dispatch`, while ingest maps
`rawpost.RawPost` to store params at the boundary. The service module never imports
the `sources/*` modules.

### Commands

```sh
make check                     # fmt + vet + lint + test + build
go test -race -tags=conformance ./...   # conformance suite (real PG via testcontainers)
make smoke                     # real-binary smoke (go test -tags=smoke ./cmd/kura-releases)
# per-module (matches CI):
for m in . sources/dmhy sources/nyaa; do (cd "$m" && GOWORK=off go build ./... && go vet ./... && go test -race ./...); done
```

### Learnings

**Accumulated corrections. This section is for the agent to maintain.** When the user
corrects your approach, append a one-line, concrete rule here before ending the session.

- The conformance suite drives in-process seams (`httptest`, direct dispatch) and **never
  boots the binary**, so deploy-shape bugs — MCP transport wiring, startup migrations,
  fail-fast bind, the graceful-drain order — are invisible to it. Validate the binary
  end-to-end with the real-binary smoke (`make smoke`), not the conformance gate alone.
- `attempt_count` means failed unmatched submissions, not claims; claim crashes must
  not affect matching semantics.

---

## services/webui — suite web UI

Static SPA (React + Vite) served by Caddy, which also reverse-proxies
`/api/*` to `KURA_WEBUI_LIBRARY_UPSTREAM`. See `services/webui/README.md`
for structure and commands (`make check` = lint + typecheck + test + build).

- `src/api/types.gen.ts` is generated by the library service's
  `make gen-ts` (tygo owns the response contract). Never hand-edit it;
  regenerate from `services/library` and commit the result.
