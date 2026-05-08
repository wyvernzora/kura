/**
 * Hand-typed mirrors of `internal/response/*.go` Go structs.
 *
 * The wire shape is the source of truth; this file is reviewed against
 * the Go side on every PR that touches the response types. PR backlog
 * item: introduce tygo (or OpenAPI codegen) once the UI shape settles
 * and drift becomes a real risk.
 */

import type { Status } from '@/lib/status';

/**
 * `internal/server/auth.writeUnauthorized`-shaped 401 envelope. Also
 * matches the broader REST error envelope used by writeError elsewhere
 * (kind drives the union; message is always populated).
 */
export interface ApiErrorEnvelope {
  kind: string;
  category?: string;
  message: string;
}

/**
 * Stable kind values the UI dispatches on. Add entries as the auth
 * flow + future error UX needs them.
 */
export const ApiErrorKinds = {
  Unauthorized: 'unauthorized',
  Forbidden: 'forbidden',
  Internal: 'internal',
  Validation: 'validation',
} as const;

/**
 * `GET /api/v1/health` — bearer-exempt by design. Returned for
 * liveness probes; the auth handshake does NOT use it (a 200 here is
 * uninformative about token state).
 */
export interface HealthResponse {
  ok: boolean;
  version: string;
  libraryRoot: string;
  uptimeMs: number;
  startedAt: string;
}

/**
 * `GET /api/v1/library` — protected. The auth probe targets this
 * endpoint: a 200 with valid JSON means the bearer is good (or anon
 * mode is in effect); a 401 with the kura error envelope means the
 * bearer is required and we should show the login screen.
 */
export interface LibraryResponse {
  libraryRoot: string;
  seriesCount: number;
  startedAt: string;
  uptimeMs: number;
}

/**
 * One row of `GET /api/v1/series`. Matches `internal/response.ListRow`.
 *
 * `metadataRef` is a provider:id string (e.g. "tvdb:370070"). Untracked
 * series have no metadataRef and are excluded from the resolve-search
 * intersect by design.
 */
export interface ListRow {
  status: Status;
  staged?: boolean;
  title: string;
  canonicalTitle?: string;
  seasonsAvailable: number;
  seasonCount: number;
  episodesAvailable: number;
  episodeCount: number;
  metadataRef?: string;
  resolutions?: string[];
  sources?: string[];
  /**
   * Series-level poster URLs lifted from `series.json` server-side.
   * Both empty when the metadata provider had no poster, or when the
   * row hasn't been scanned since posters were surfaced; the UI then
   * falls back to the deterministic generated art.
   */
  posterUrl?: string;
  posterThumbnailUrl?: string;
  lastScanned?: string;
  /**
   * Server-folded fuzzy-search blob — newline-joined token list of
   * alias / translated-title material the home page feeds into
   * fuse.js. Never displayed; missing on rows that haven't been
   * scanned since schema v3 landed (legacy v2 falls back to title-
   * field search alone).
   */
  searchKey?: string;
  error?: string;
}

/**
 * `GET /api/v1/series` response envelope. Cursor-paginated; clients
 * walk `nextCursor` until empty to assemble the full library. The
 * `dataChanged` flag is set when the index changed between pages
 * (consumers may want to restart pagination if strict ordering
 * matters).
 */
export interface ListResult {
  rows: ListRow[];
  nextCursor?: string;
  dataChanged?: boolean;
}

/**
 * One ranked candidate from `POST /api/v1/resolve`. Mirrors
 * `internal/response.Candidate`. `ref` is the metadata identifier the
 * library home intersects against `ListRow.metadataRef` to surface
 * matches for the active search query.
 */
export interface Candidate {
  ref: string;
  preferredTitle: string;
  canonicalTitle?: string;
  year?: number;
  firstAired?: string;
  originalLanguage?: string;
  originalCountry?: string;
  genres?: string[];
  evidence?: Evidence[];
}

/**
 * Per-term ranking annotation on a candidate. Surfaces use this for
 * tie-breaking and "why did this match" UI; P4 just uses presence in
 * the candidate list and array order.
 */
export interface Evidence {
  term: string;
  rank: number;
  matchSource?: string;
  annotations?: string[];
}

/**
 * `POST /api/v1/resolve` response. Outcome encoded by candidate-list
 * cardinality:
 *
 *   - empty       → not found
 *   - one         → unique match
 *   - many        → ambiguous (caller picks)
 *
 * The library home treats all non-empty results as a search index over
 * the loaded library; ranking comes from array order.
 */
export interface Resolution {
  candidates: Candidate[];
}

/**
 * `POST /api/v1/resolve` request body.
 */
export interface ResolveRequest {
  terms: string[];
}

/**
 * `GET / POST / DELETE /api/v1/series/{ref}/aliases` response shape.
 * The list is the persisted user shorthands for the addressed
 * series; TVDB-derived aliases never appear here.
 */
export interface AliasList {
  aliases: string[];
}

/**
 * Body shared by `POST` (add) and `DELETE` (remove) on the aliases
 * endpoint.
 */
export interface AliasMutation {
  aliases: string[];
}

/**
 * Episode-level status — mirrors `internal/response.Status`.
 *   - `pending` — episode aired-date is in the future.
 *   - `missing` — aired but no file present.
 *   - `present` — file present, indexed, identified.
 *   - `staged` / `staged_replacement` — staged file pending the next
 *     reconcile-apply.
 */
export type EpisodeStatus = 'pending' | 'missing' | 'present' | 'staged' | 'staged_replacement';

/**
 * `GET /api/v1/series/{ref}` — full series detail. `internal/response.Show`.
 *
 * `metadataRef` is provider:id (e.g. `tvdb:370070`) — the same identifier
 * used in the library list. `ref` is the storage SeriesRef (folder
 * name); we surface it for display only, server resources address by
 * metadata ref.
 */
export interface Show {
  metadataRef: string;
  ref: string;
  root: string;
  lastScanned?: string;
  preferredTitle: string;
  canonicalTitle?: string;
  status: Status;
  artwork?: ArtworkShow;
  seasons: SeasonShow[];
  truncated?: boolean;
  truncatedRanges?: string[];
  truncationHint?: string;
  stagedTrash?: TrashItemShow[];
  stagedExtras?: ExtraItemShow[];
}

export interface ArtworkShow {
  poster?: PosterShow;
}

export interface PosterShow {
  url: string;
  thumbnailUrl?: string;
  language?: string;
}

/**
 * Season number 0 is specials. Episodes is omitted on the wire when
 * the caller asked for season summaries only; we always render with
 * an array (treat undefined as []).
 */
export interface SeasonShow {
  number: number;
  summary: SeasonSummary;
  episodes?: EpisodeShow[];
}

export interface SeasonSummary {
  episodeCount: number;
  present: number;
  missing: number;
  staged: number;
  stagedReplacement: number;
  pending: number;
}

/**
 * `episode` is the marker string `S01E0003` (storage form). Display
 * code may want to parse season / episode numbers; helpers for that
 * live in `lib/episodeRef.ts` if added.
 */
export interface EpisodeShow {
  episode: string;
  aired?: string;
  status: EpisodeStatus;
  preferredTitle?: string;
  canonicalTitle?: string;
  active?: MediaShow;
  staged?: MediaShow;
}

export interface MediaShow {
  source: string;
  resolution?: string;
  codec?: string;
  size: number;
  file: string;
  companions: CompanionShow[];
}

export interface CompanionShow {
  path: string;
  role?: string;
  language?: string;
  label?: string;
  size: number;
  mtime: string;
}

export interface TrashItemShow {
  id: string;
  path: string;
  size: number;
  mtime: string;
  addedAt?: string;
  companions?: CompanionShow[];
}

export interface ExtraItemShow {
  id: string;
  season: number;
  path: string;
  prefix?: string;
  isDir: boolean;
  addedAt?: string;
}
