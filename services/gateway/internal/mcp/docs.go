package mcp

// Tool descriptions and server instructions.
//
// These are the agent-facing prose for the whole suite: one server, one
// instruction text, merged from what the two leaf MCP servers carried
// separately. No tool name here refers to a leaf-era `kura_*` name.

const instructions = `Kura manages an anime library and a release index behind one API.

Identity. A tracked series is named by its metadata ref, a provider:id string
such as tvdb:370070. That ref is the ` + "`ref`" + ` argument for every series tool.
When you have a title rather than a ref, call resolve_series first; do not
guess a ref. On-disk directory names are not addresses and most tools do not
return them.

Paths. Every path-bearing value is a scheme-tagged selector, never a bare
filesystem path: ` + "`inbox:<rel>`" + ` for a file under the download inbox,
` + "`series:<rel>`" + ` for one inside a series directory. Pass them back exactly as
received. You cannot reach a file outside those roots; if a file needs staging
and is elsewhere, ask the user to move it into the inbox.

Long work is asynchronous. scan_series, stage_series_media, and
apply_series_reconcile return a jobId immediately. Poll get_job for state.
On success get_job omits the result: read the outcome through get_series
instead. On failure it includes what the job managed to do, so you can correct
the input and retry.

Reconcile is two steps on purpose. plan_series_reconcile returns a token
describing what would move; apply_series_reconcile executes exactly that plan.
Show the plan to the user before applying it. A token is spent once.

Not available here. Permanent deletion and repair are outside this surface:
emptying or restoring trash, recovering a stuck reconcile, library-wide reindex
or rescan, and release ingestion or queue operations are all operator work
through the CLI or REST. Untracking a series has no verb on any surface — it is
a filesystem operation the operator performs directly. If a task needs one of
these, say so rather than looking for a tool.

Tags are opaque workflow labels. The suite convention is namespaced:
priority:high, priority:low, maintenance:requested, maintenance:disabled.`

const docResolveSeries = `Resolve free text or an exact provider ref to candidate series.

Call this first whenever you have a title rather than a provider:id ref. Terms
are matched against the metadata provider, so this reaches the network and can
be slower than the library-local tools.

Returns candidates with their refs; pick one and pass its ref to the other
series tools. An exact ref like tvdb:370070 resolves to itself.`

const docListSeries = `List tracked series with optional filters.

Filters are conjunctive: status, the observed-airing flag, and tags all narrow
the same page. Returns at most 100 rows regardless of the limit requested; page
with the returned nextCursor.

This reads the library index only and never contacts the metadata provider.`

const docGetSeries = `Get one series with its seasons, episodes, and media state.

Use the episodes selector (for example S01 or S01E03-E05) to narrow large
series; a response that would exceed the budget is refused with the budget in
its data rather than truncated into invalid JSON, and truncatedRanges in a
successful response can be passed straight back as episodes.

Returns the agent-facing view: ref, titles, tags, artwork, seasons, per-episode
state, active and staged media, and staged trash and extras. Host-side fields —
the on-disk directory, the library root, the generation counter — are omitted.`

const docUpdateTags = `Add and remove opaque workflow tags on a series.

Each entry in tags is a change, not a final state: a plain tag adds it, a !tag
removes it. Both happen atomically. Tags are lower-cased.

Returns the resulting tag set.`

const docAddSeries = `Add a new series to the library.

Creates the directory and writes its metadata from the provider, so this
reaches the network. The ref must be an exact provider:id; resolve it with
resolve_series first if you only have a title.

Use directory only to override the derived directory name. This creates
tracked state and is not idempotent — a second call for the same ref conflicts.`

const docImportSeries = `Adopt an existing untracked directory as a series.

The directory must already exist under the library root and must not already
carry Kura metadata. Binds the given ref to it and writes the metadata spine
from the provider.

Overwriting a tracked series' metadata is deliberately not available here; that
remains operator work through the CLI or REST.`

const docScanSeries = `Scan a series directory for episode media. Asynchronous.

Records recognized files against episode slots and refreshes changed facts.
refresh re-probes media even when size and modification time are unchanged;
metadataOnly skips the filesystem walk and refreshes provider data only.

Returns a jobId. Poll get_job, then read the result through get_series.`

const docStageMedia = `Stage media, trash, or extras for a series. Asynchronous.

Staging records intent; nothing moves on disk until a reconcile is planned and
applied. Media paths are inbox: or series: selectors exactly as returned by
list_inbox or get_series.

Returns a jobId. Poll get_job, then plan a reconcile to see what would move.`

const docResetStaging = `Drop staged records from a series.

Clears intent only — no media on disk is touched. Target one episode with
episode, specific staged items with trashIds or extraIds, or everything on the
series with all.

Returns how many records were cleared and which trash and extra ids went with
them.`

const docPlanReconcile = `Plan the filesystem changes a series needs.

Compares staged intent against the current layout and returns a token plus the
moves it describes: media into place, replaced files into trash, extras into
their season folders. Nothing is executed.

Show the plan to the user before applying. Pass the token to
apply_series_reconcile; an empty plan means there is nothing to do.`

const docApplyReconcile = `Apply a reconcile plan. Asynchronous.

Executes exactly the plan the token describes — the token is a snapshot hash,
so a library that changed since planning is rejected rather than partially
applied. A spent token cannot be reused.

Moves media and places replaced files in trash. Returns a jobId; poll get_job.`

const docGetJob = `Get the state of an asynchronous job.

Always returns state, progress, and any error. The result is omitted while the
job is running and after it succeeds — read the outcome through get_series or
list_releases instead. When the job failed, the result is included so you can
see how far it got and correct the input before retrying.`

const docListInbox = `List files in the download inbox awaiting staging.

Returns at most 500 entries regardless of the limit requested. Dotfiles and
in-flight download markers are hidden unless includeHidden is set. Accepts an
exact file path as well as a directory.

Entry paths come back as inbox: selectors; pass them to stage_series_media
unchanged.`

const docListReleases = `List matched releases, newest first.

Narrow to one series with ref, or omit it for recent matches across the whole
index. since pages by first-matched time instead of publication time, which is
what you want when catching up on what was matched recently.

Returns at most 100 releases regardless of the limit requested; page with the
returned nextCursor. sizeBytes and confidence are null when unrecorded — that
is not the same as zero. Magnets are not included; use get_magnet.

A cursor belongs to the call that produced it: it is bound to that ref and to
whether since was present, because those select different orderings. Pass back
the nextCursor from the response you just received, with the same ref and the
same since argument. Reusing one against a different series, or after adding or
dropping since, is rejected as invalid_cursor rather than silently ignored — so
when you change either, start again with no cursor.`

const docGetRelease = `Get one release with its full context.

Returns the representative fields, match status and history, and every raw
source posting linked to the release. Nullable facts stay explicitly null
rather than being omitted, so you can tell "not recorded" from "not returned".

This includes the magnet, but it is a large response — if you only need the
magnet, call get_magnet instead.`

const docGetMagnet = `Get the magnet URI for one release.

Takes a canonical 40-hex v1 btih infohash and returns just that release's
magnet. Use this rather than get_release when handing a magnet to a download
client: get_release returns the full detail including raw postings and match
history.

There is no bulk form; call once per release.`
