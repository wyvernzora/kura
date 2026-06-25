# design-sync notes — @kura/web → claude.ai/design

Kura's `web/` is a Vite **application**, not a component library. The
storybook-shape converter assumes a published DS (dist + .d.ts). Everything
below bridges that gap. Read before any re-sync.

## How this repo is wired into the converter

- **Bundle entry is a hand-written barrel**: `web/.design-sync-entry.tsx`
  (committed). It `export *`s every storied component so they land on
  `window.KuraWeb`, plus `StoryProviders` + `StoryRouter` for `cfg.provider`.
  Story-helper modules (`_storyProviders`, `_fixtures`, `_storyRouter`, …) are
  deliberately NOT exported — they must compile into each preview from source.
  If you add a storied component, add an `export *` line here.
- **`web/index.d.ts` is a stub types entry** (committed). The converter scans a
  package `.d.ts` to (1) curate which storybook components are public and (2)
  extract prop bodies. This app ships no `.d.ts`, so the stub declares every
  storybook title-leaf name as `(props:any)=>any`. Consequence: emitted
  `<Name>.d.ts`/Props are LOOSE (`any`). Real API guidance comes from the
  verified previews + story-source examples in `<Name>.prompt.md`. If you
  add/rename a storied component, update this stub (a re-sync flags drift as
  `[TITLE_UNMAPPED]`).
- **`tsconfig` = `tsconfig.app.json`** so esbuild resolves the `@/*` → `src/*`
  alias when bundling the barrel.

## [GENERAL] global fixes (already in config — keep them)

- **CSS: Tailwind v4 is runtime-injected, so static scraping misses it.** The
  `@theme` tokens + all utility classes live in Vite's JS-injected `<style>`,
  not a static `<link>` — the converter only caught the CSS-module + font CDN
  links (11KB, no tokens → `[TOKENS_MISSING]`). Fix: `pnpm build` compiles the
  full CSS to `web/dist/assets/*.css`; `buildCmd` concatenates them to
  `web/.ds-styles.css` (gitignored artifact) and `cfg.cssEntry` points at it.
  ALWAYS re-run `buildCmd` before the converter on a re-sync (it rebuilds this).
- **Provider: dual react-query instance.** Components use react-query/router via
  the component bundle's copy. Story helpers (`withStoryProviders`) bundle a
  SECOND react-query whose context the components never see → provider-coupled
  components (ScanButton, SeriesDetail, SeriesPosterCard, GearMenu, …) rendered
  blank. Fix: `StoryProviders`+`StoryRouter` are exported from the barrel (same
  bundle as components) and wired via `cfg.provider` (StoryRouter > Story
  Providers), so previews share one instance. Setting `cfg.provider` also makes
  the converter SKIP decorator bundling (which failed anyway on the
  `@import "tailwindcss"` in `globals.css`).
  - Side effect of skipping decorators: the theme decorator (`data-k-theme`) and
    `fetchMock` no longer run in previews. `paper` (light) is the CSS default so
    theme is fine. `fetchMock` absence means real fetches fail — watch
    SeriesDetail (does a live-ish show fetch → 502) and scan stories.
- **react-query single instance (`cfg.storyImports.shim`)**: stories that SEED
  the query cache (`SeriesDetail`'s `withSeededShow` does `new QueryClient()` +
  `setQueryData`) seed a preview-only react-query the components never read →
  the component stays on the fetch/404 branch. Fix: barrel `export * from
  '@tanstack/react-query'` + `cfg.storyImports.shim: ["@tanstack/react-query"]`
  so story imports resolve to the bundle's single instance. This is what makes
  SeriesDetail render its data stories. Don't remove either half.
- **GearMenu skipped (`sb-error`)**: all 3 GearMenu stories crash in the
  REFERENCE storybook ("Cannot read properties of null (reading 'stores')") —
  the static isolated `?story=` render lacks setup the live storybook supplies.
  No gradeable ground truth → `cfg.overrides.GearMenu.skip` all 3 → ships as a
  floor card. Not fixable from sync config (it's the repo's storybook).
- **TVDB posters 403 in this environment**: `artworks.thetvdb.com` rejects
  requests here (`[ASSETS_BLOCKED]`), so real poster images load on NEITHER
  panel. Harmless in practice: `PosterArt` renders procedural fallback art
  (identical both sides), so SeriesPosterCard/SeriesDetail/Poster grade fine on
  layout. Real-poster rendering is UNVERIFIED here — would need TVDB egress.
- **`[REFERENCE_STALE?]` after sync-only edits is expected**: editing the
  barrel/config changes the bundle but not component source — the reference is
  still valid. Only rebuild sb-reference when `web/src` component source changes.
- **QualityChip**: the story is a composite of `ResolutionChip`/`SourceChip`
  (no `QualityChip` export). `cfg.titleMap {QualityChip: ResolutionChip}` binds
  the card to a real export; stub lists `ResolutionChip`.
- **GRID_OVERFLOW overrides** (`cfg.overrides`): overlays GearMenu +
  ScanDetailsModal → `cardMode: single`; wide stories SearchField, Card, Poster,
  EpisodeRow, SeasonPanel, StatusDot → `cardMode: column`.

## [GENERAL] grading learnings (folded from fan-out)

- **Router/store-context components render correctly in the PREVIEW but their
  REFERENCE storybook crashes** (Logo, TopBar, AppShell, GearMenu). The static
  isolated `?story=` render in sb-reference throws "Cannot read properties of
  null (reading 'stores')" (TanStack Router) or shows storybook's "Something
  went wrong!" boundary — sometimes flaky, mostly deterministic. `cfg.provider`
  gives the PREVIEW its router/query, so previews are faithful (verified
  standalone via screenshots of `ds-bundle/components/.../<Name>.html`). These
  are graded `match` with a disclosure note (reference unusable, preview
  verified standalone). NOT a sync defect — it's the repo storybook's static
  isolation limit. Logo's story has no router decorator at all yet the component
  calls `useRouterState`, so its reference can never render.
- **Arbitrary-value Tailwind classes in story decorators don't apply in
  previews.** Tailwind v4 JIT only emits classes it scraped from `web/src`, not
  classes that exist only in story files. A story decorator that sizes a
  component with `w-[180px]`/`aspect-[0.7]`/`grid-cols-4`/`shadow-poster` etc.
  ships none of those → components relying on a parent for size (absolute fills,
  `w-full h-full`) collapse/stretch. Fix: own the preview
  (`.design-sync/previews/<Name>.tsx`) and replace decorator layout classes with
  inline styles. Done for **PosterArt** (owned preview, committed).
- **[PORTAL?] overlay/modal components** (ScanDetailsModal, Radix Dialog): the
  reference captures only the trigger (the portal mounts to `document.body`,
  outside the captured node). The preview renders the full modal; grade it
  against the story-source fixtures. `cardMode: single` is set for these.
- When a contact sheet downscales a column, read the full-res `raw/*__sb.png` /
  `raw/*__ds.png` before calling a small-text delta a mismatch.

## Re-sync risks (watch these)

- `web/index.d.ts` and the barrel are MANUALLY maintained — story add/rename
  silently drops or mis-binds a component until both are updated.
- Props are `any` everywhere (no real `.d.ts`). If real prop types become
  important, emit declarations (tsc) and repoint the types entry.
- `.ds-styles.css` is a build artifact — a stale one ships old styling. buildCmd
  regenerates it; never hand-edit.
- fetchMock/theme decorator do not run in previews (cfg.provider skips them).
- Fonts load from Google Fonts CDN via `@import` (`[FONT_REMOTE]`) — grading
  must run from a shell with egress, or both panels fall back identically and
  hide a font regression. (Google Fonts reachable in this env; TVDB images 403.)
- The 4 router/store components (Logo, TopBar, AppShell, GearMenu) were graded
  by STANDALONE preview inspection (no storybook reference). A real regression
  in them would not be caught by image-pair compare — re-verify by eye on
  re-sync if their source changes.
- **PosterArt owned preview** uses inline styles to substitute for story-only
  arbitrary-value Tailwind classes. If PosterArt's API changes, update
  `.design-sync/previews/PosterArt.tsx`. A `[REFERENCE_STALE?]`-flavored
  divergence was seen on the standalone PosterArt sheet (procedural art); the
  preview matches current `web/src` and in-context (SeriesPosterCard, Poster)
  matches both panels — re-examine if it resurfaces.
- SeriesDetail's `error-state` story and all GearMenu stories' reference renders
  are skipped/unusable — see overrides + standalone-grade notes.
