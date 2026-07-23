# AGENTS.md

Drop-in operating instructions for coding agents working on **Kura**. Read the user-global rules first:

- `~/.agents/AGENTS.md` — universal agent-behavior rules (non-negotiables, simplicity, surgical changes, communication style, grilling, etc.)
- `~/.agents/go.md` — Go engineering rules (loaded because this repo has `go.mod`)

This file holds project-specific context, learnings, and overrides only. Rules in the global files apply unless explicitly contradicted here.

---

## 1. Project-specific overrides

### End-to-end tests

- E2E tests, once authored, are load-bearing regression contracts. ANY change to an existing e2e scenario — whether the work is related or unrelated, whether the change is one assertion or a rewrite, whether or not a feature change "requires" the update — requires explicit user approval before the edit. The agent must surface the proposed change with a detailed explanation of (a) what behavior the scenario currently asserts, (b) what is changing in the scenario, and (c) why the system change demands the scenario change rather than a system fix. No exceptions. If a scenario fails because the system under test changed, surface the failure for review; do not mutate the assertions to turn red into green.
- When the user explicitly reports that the app is not behaving as required (a bug report, "this is wrong," "shouldn't do X"), add an e2e regression scenario alongside the fix when feasible. The scenario reproduces the reported behavior in its red state and asserts the corrected behavior in its green state. Skip only when the bug genuinely cannot be exercised through the e2e harness; surface that limitation in the response so the user can decide whether to add coverage some other way.

---

## 2. Project context

### About Kura

- **Name:** Kura.
- **Domain:** anime-first library manager, broadly similar in category to Sonarr.
- **Priority:** anime behavior comes first; other series types can work when compatible but should not drive the design.
- **Product shape:** no bloat. Prefer CLI tools for manual use and MCP tools for agentic use.
- **Operational scale:** personal anime library automation, not a high-throughput multi-writer file transaction system. Expect new episodes a few times a week and occasional season upgrades, usually flowing qbit/download inbox -> Kura -> library with an LLM agent driving Kura. Kura is the only intended writer inside the library root; other tools should be readonly there, and direct human writes are rare. Avoid engineering for AWS-S3-scale concurrency or hostile library writers unless the product requirements explicitly change.
- **UI:** a web UI (`web/`, pnpm + vite) is built and embedded into the binary (`internal/server/webui`), served by `kura serve --rest` at `/`.
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
- **Transports:** `kura serve` hosts the servers: `internal/server/mcp` (MCP tool surface, stdio or streamable HTTP), `internal/server/rest` (REST API under `/api/*`, bearer-token auth via `internal/server/auth`, embedded web UI from `internal/server/webui` at `/`). Servers depend on `internal/workflow` for behavior. `cmd/kura` hosts the CLI; its verbs are REST clients via `internal/cli/client` (discovery through `KURA_SERVER_URL`, default `http://127.0.0.1:8080`) — only the `serve` command imports `internal/workflow` in-process.
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

### Documentation

- `scratch/` — gitignored agent scratch directory. Contains coding-agent context, specs, plans, and local notes. Read these before starting non-trivial work; update them when context shifts.
- `scratch/local.md` — local notes for actual mount paths and machine-specific details.
- Use generic public examples in committed docs (`/media/anime/series`, `/media/anime/inbox`). Personal mount paths stay in `scratch/`.

### Forbidden

- Active tracked media under `.kura/` (only Kura-managed trash is allowed there).
- Bare `.series.json` outside `.kura/`.
- Adding `providerRefs`, local series IDs, or `preferredProvider` to series metadata.
- Background magic in lieu of explicit CLI/MCP surfaces.

---

## 3. Project Learnings

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
- Commit subjects use Conventional Commits with the closed scope enum from the root AGENTS.md; the former `<scope>: <message>` convention is retired (2026-07, monorepo migration).
- Run LTO/LTFS tooling on a dedicated VM with its own read-only `KURA_LIBRARY_ROOT` mount and VM-local disposable tape state; derive hot/cold placement from payload-change history rather than raw metadata-write frequency.
- Treat LTO as operator-assisted homelab archival for valuable-but-replaceable data; prefer visible manual recovery conflicts and operator-approved risk over high-availability protocols or automatic conflict resolution.
