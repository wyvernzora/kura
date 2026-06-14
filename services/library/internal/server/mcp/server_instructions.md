Kura is an anime-first library manager for a personal media library. It exposes the same workflow core through CLI, REST, and MCP; this MCP server is the agent-facing surface.

## Common rules

These rules are correctness requirements. Violating them can make you act on the wrong series or pass paths Kura cannot resolve.

- Never search, join, filter, or compare the Kura library by raw series names, release titles, aliases, or fuzzy string matches. If you have anything other than an explicit metadata ref, call `kura_resolve` first and use the chosen metadata ref.
- Never invent, normalize, romanize, translate, trim, lowercase, uppercase, re-encode, or otherwise rewrite metadata refs, selector strings, series names, episode titles, or inbox paths. Copy strings returned by Kura tools verbatim.
- Never reconstruct `inbox:`, `series:`, or `library:` selectors from displayed path fragments. Use selector strings exactly as returned by Kura.
- Never assume `kura_show` proves files still exist. It reports Kura's recorded state; use `kura_scan` when current file presence matters.
- Never treat a submitted async job as successful until `kura_job_status` reports a terminal `succeeded` state.

## Core model

- A tracked series has one provider identity, the metadata ref, such as `tvdb:370070`. Metadata refs are the normal `ref` value for MCP tools.
- The current metadata provider is TVDB. The provider supplies series identity, titles, artwork, episode slots, and air dates.
- Each series has an episode spine: the set of known episode slots. Local media records attach to spine slots.
- A series can also be untracked: Kura can see it in the library, but it has not been adopted into Kura metadata yet.
- `kura_show` reads persisted Kura metadata. It does not verify files are still present. Run `kura_scan` when disk state may have changed.

Episode status values:

- `pending`: the episode has not aired and no media is recorded.
- `missing`: the episode has aired and no media is recorded.
- `present`: active media is recorded.
- `staged`: media is queued to become active.
- `staged_replacement`: staged media is queued to replace active media.

Library-list row status values:

- `complete`: every currently trackable aired episode has active media.
- `incomplete`: at least one currently trackable aired episode is missing media, or the series has no episodes.
- `untracked`: Kura can see the series, but does not manage it yet.
- `error`: Kura could not compute the row; the row carries an error message.

`isAiring` is an independent row flag, not a row status. `staged` is also independent of row status.

## Selectors and paths

- Use `kura_resolve` when you only have a title or release name. Resolve returns metadata candidates; after choosing one, call other tools with the chosen metadata ref.
- Tools that take `ref` expect exactly one metadata ref. Do not invent refs.
- Kura tools do not use caller-local absolute paths. Path fields are scheme-tagged selectors:
  - `inbox:<rel>` identifies media visible through Kura's inbox.
  - `series:<rel>` identifies media inside the selected series.
  - `library:<rel>` identifies a library-scoped item.
- Copy selector strings from tool output exactly, especially for non-ASCII paths.

## Lifecycle

- `kura_add` creates a new tracked series and initial episode spine.
- `kura_import` marks an existing untracked series as tracked and writes the initial spine; it does not adopt existing media until scan.
- `kura_scan` refreshes provider metadata, inspects the selected series' files, adopts files that match Kura's episode layout, updates changed active media facts, prunes active records whose files disappeared, and reports skipped files. It does not move files.
- `kura_stage` records intent for one series. It can stage episode media, queue series files for trash, and queue extras for placement. Files stay where they are until reconcile apply.
- `kura_reset` removes staged intent. It does not touch active media files.
- `kura_reconcile_plan` computes and persists the moves needed to apply current staged intent. A non-empty plan returns a token; an empty plan returns no token.
- `kura_reconcile_apply` applies a saved plan: staged episode media moves into canonical locations, replaced active media moves into Kura trash, queued trash files move into Kura trash, and queued extras are placed as extras.

Long-running workflows return a `jobId`. Poll `kura_job_status` until the job reaches `succeeded`, `failed`, or `cancelled`. Current async MCP tools are `kura_scan`, `kura_stage`, and `kura_reconcile_apply`.

## Safety boundaries

- Read tools do not mutate library state.
- Staging and reset mutate Kura metadata only; they do not move media files.
- Reconcile apply moves files but displaces active media into recoverable Kura trash rather than permanently deleting it.
- Permanent deletion and operator repair workflows are outside the MCP tool surface. If a task requires trash empty, trash restore, purge remove, or stale reconcile recovery, surface that to the user.
