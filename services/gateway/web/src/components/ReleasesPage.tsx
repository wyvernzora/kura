import * as DialogPrimitive from '@radix-ui/react-dialog';
import { ChevronDown } from 'lucide-react';
import { type ReactNode, useEffect, useMemo, useRef, useState } from 'react';

import { apiErrorMessage } from '@/api/client';
import { useResolveSeries, useSeriesList } from '@/api/hooks';
import {
  useQueueStats,
  useRefreshReleases,
  useReleaseDetail,
  useReleaseStream,
  useSetReleaseStatus,
} from '@/api/releaseHooks';
import type {
  MatchEvent,
  MatchStatus,
  QueueStats,
  RawItem,
  ReleaseDetail,
  ReleaseItem,
} from '@/api/releaseTypes';
import type { ListRow } from '@/api/types';
import { SearchField } from '@/components/SearchField';
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { GhostIconButton } from '@/components/ui/ghost-icon-btn';
import { MaterialIcon } from '@/components/ui/material-icon';
import { cn } from '@/lib/cn';
import { formatBytesDecimal, formatCompactAge, formatStampUTC } from '@/lib/format';
import { composeReleaseRef, type EpisodeMarker, parseEpisodeMarker } from '@/lib/releaseEpisode';
import {
  confidenceColor,
  confidencePercent,
  isLowConfidence,
  LOW_CONFIDENCE,
  MATCH_STATUSES,
  RELEASE_PAGE_SIZE,
  RELEASES_REFRESH_EVENT,
  type ReleaseActionId,
  releaseActions,
  STATUS_DOT,
  STATUS_LABELS,
  seriesTitleForRef,
  seriesTitleIndex,
} from '@/lib/releases';
import { searchLibrary } from '@/lib/searchLibrary';
import { PosterArt } from '@/poster/PosterArt';
import {
  mergeReleaseStreams,
  nextStreamToLoad,
  type ReleaseFilterKey,
  streamsForFilter,
} from '@/state/releaseStreams';
import { ATTENTION_FILTER, useReleasesView } from '@/state/releasesView';

/** Candidates shown at once in the hand-match search. */
const CANDIDATE_LIMIT = 6;

/**
 * Release attention — releases the matcher could not finish.
 *
 * Two states are asking for a decision: `exhausted` (the attempt
 * budget ran out) and `matched` below the confidence threshold
 * (auto-matched but not trusted). `suppressed` is inspection material
 * rather than a queue — it is not actionable on its own and the only
 * way out is a hand match — and `dead` is downloader-owned. Both have
 * their own filter entry and neither is in the default view.
 *
 * There are no stat tiles: at personal scale the list IS the summary,
 * and per-status counts live in the filter menu where they scope
 * something. There is no auto-refresh either — a queue that reorders
 * itself while the operator is deciding is worse than a stale one.
 */
export function ReleasesPage() {
  const active = useReleasesView((s) => s.active);
  const toggle = useReleasesView((s) => s.toggle);
  const showAll = useReleasesView((s) => s.showAll);
  const reset = useReleasesView((s) => s.clear);

  const [visible, setVisible] = useState(RELEASE_PAGE_SIZE);
  const [openInfohash, setOpenInfohash] = useState<string | null>(null);
  const [pending, setPending] = useState<PendingAction | null>(null);
  const [result, setResult] = useState<ReleaseOutcome | null>(null);
  const [refreshedAt, setRefreshedAt] = useState(() => formatStampUTC(new Date().toISOString()));

  const specs = streamsForFilter(active);
  const plain = useReleaseStream(specs.find((s) => s.key === 'plain') ?? null);
  const lowconf = useReleaseStream(specs.find((s) => s.key === 'lowconf') ?? null);
  const stats = useQueueStats();
  const series = useSeriesList();
  const detail = useReleaseDetail(openInfohash);
  const setStatus = useSetReleaseStatus();
  const refetchAll = useRefreshReleases();

  const streams = [plain, lowconf].filter((stream) => stream.hasMore || stream.items.length > 0);
  const merged = mergeReleaseStreams(streams);
  const page = merged.items.slice(0, visible);
  const loading = plain.isPending || lowconf.isPending;
  const failure = plain.isError ? plain.error : lowconf.isError ? lowconf.error : null;

  const titles = useMemo(() => seriesTitleIndex(series.data ?? []), [series.data]);

  // The shell owns the mobile page bar, so its refresh button
  // announces itself on the window rather than threading a callback
  // down through AppShell into this route. The handler goes through a
  // ref so the subscription is set up once while still calling the
  // current render's closure.
  const refreshRef = useRef(refresh);
  refreshRef.current = refresh;
  useEffect(() => {
    const onRefresh = () => refreshRef.current();
    window.addEventListener(RELEASES_REFRESH_EVENT, onRefresh);
    return () => window.removeEventListener(RELEASES_REFRESH_EVENT, onRefresh);
  }, []);

  // Opening a release starts at its top; coming back restores where
  // the list was.
  const listScroll = useRef(0);
  useEffect(() => {
    window.scrollTo(0, openInfohash ? 0 : listScroll.current);
  }, [openInfohash]);

  function refresh() {
    setRefreshedAt(formatStampUTC(new Date().toISOString()));
    refetchAll();
    setResult({
      kind: 'success',
      title: 'Queue refreshed',
      detail: `${merged.items.length} ${merged.items.length === 1 ? 'release' : 'releases'} in view`,
    });
  }

  function toggleFilter(key: ReleaseFilterKey) {
    toggle(key);
    setVisible(RELEASE_PAGE_SIZE);
  }

  function loadMore() {
    // Rows already fetched but not yet shown come for free; otherwise
    // advance whichever stream is holding the merge back.
    if (visible >= merged.items.length) {
      const next = nextStreamToLoad(streams);
      if (next === 'lowconf') {
        lowconf.fetchNextPage();
      } else if (next === 'plain') {
        plain.fetchNextPage();
      }
    }
    setVisible((n) => n + RELEASE_PAGE_SIZE);
  }

  async function runAction(input: { ref?: string; reason: string }) {
    if (!pending) {
      return;
    }
    const { action, release } = pending;
    setPending(null);
    const target: MatchStatus = action === 'requeue' ? 'unmatched' : 'matched';
    const ref = action === 'affirm' ? (release.ref ?? undefined) : input.ref;
    try {
      await setStatus.mutateAsync({
        infohash: release.infohash,
        body: {
          status: target,
          ...(target === 'matched' && ref ? { ref } : {}),
          ...(input.reason ? { reason: input.reason } : {}),
        },
      });
      setResult({
        kind: 'success',
        title:
          action === 'match'
            ? `Matched to ${ref}`
            : action === 'affirm'
              ? 'Match affirmed at 100% confidence'
              : 'Requeued — back in the claim queue',
        detail:
          action === 'affirm'
            ? `${release.ref} · recorded to match_events`
            : `${release.matchStatus} → ${target} · recorded to match_events`,
      });
    } catch (err) {
      // The API's 409 invalid-transition message is the whole
      // explanation (which transition, and that the status moved since
      // this page loaded) — reworded it would be worth less.
      setResult({
        kind: 'error',
        title: 'Transition rejected by the API.',
        detail: apiErrorMessage(err, 'the status change failed'),
      });
    }
  }

  const open = detail.data && detail.data.infohash === openInfohash ? detail.data : null;

  return (
    <div className="mx-auto w-full max-w-[1000px] px-4 pt-5 pb-10 sm:px-6">
      {openInfohash ? (
        <ReleaseDetailView
          release={open}
          loading={detail.isPending}
          error={
            detail.isError ? apiErrorMessage(detail.error, 'could not load this release') : null
          }
          seriesTitle={seriesTitleForRef(titles, open?.ref)}
          onBack={() => setOpenInfohash(null)}
          onAction={(action, release) => setPending({ action, release })}
        />
      ) : (
        <>
          <ReleasesHeader refreshedAt={refreshedAt} onRefresh={refresh} />
          <div className="mb-4 flex flex-wrap items-center gap-2">
            <StatusFilter
              active={active}
              counts={stats.data}
              onToggle={toggleFilter}
              onReset={() => {
                reset();
                setVisible(RELEASE_PAGE_SIZE);
              }}
            />
            <span className="ml-1 text-[13px] text-muted">
              {merged.items.length} {merged.items.length === 1 ? 'release' : 'releases'}
              {merged.hasMore ? ' loaded' : active.size > 0 ? ' in this filter' : ''}
            </span>
          </div>
          {failure ? (
            <ReleasesBlank
              icon="error"
              title="Could not load the queue"
              sub={apiErrorMessage(failure, 'the release indexer did not answer')}
            />
          ) : loading ? null : merged.items.length === 0 ? (
            <ReleasesBlank
              icon="inbox"
              title="Nothing needs attention"
              sub="Nothing is exhausted or matched below confidence. Suppressed releases, if any, are listed under their own filter — they are inspection material, not a queue."
            >
              {active.size > 0 && (
                <button
                  type="button"
                  onClick={() => {
                    showAll();
                    setVisible(RELEASE_PAGE_SIZE);
                  }}
                  className={cn(
                    'inline-flex h-9 cursor-pointer items-center gap-2 rounded-md border border-line-soft bg-surface px-3 text-sm font-medium text-ink shadow-card',
                    'transition-[transform,box-shadow] duration-[160ms] hover:-translate-y-px hover:shadow-card-hover',
                  )}
                >
                  <MaterialIcon name="filter_alt_off" size={16} />
                  Show all statuses
                </button>
              )}
            </ReleasesBlank>
          ) : (
            <>
              <div className="overflow-hidden rounded-[12px] border border-line-soft bg-surface shadow-card">
                {page.map((release) => (
                  <ReleaseRow
                    key={release.infohash}
                    release={release}
                    seriesTitle={seriesTitleForRef(titles, release.ref)}
                    onOpen={() => {
                      listScroll.current = window.scrollY;
                      setOpenInfohash(release.infohash);
                    }}
                  />
                ))}
              </div>
              {(visible < merged.items.length || merged.hasMore) && (
                <div className="mt-4 flex justify-center">
                  <button
                    type="button"
                    onClick={loadMore}
                    disabled={plain.isFetchingNextPage || lowconf.isFetchingNextPage}
                    className={cn(
                      'inline-flex h-9 cursor-pointer items-center gap-2 rounded-md border border-line-soft bg-surface px-3.5 text-sm font-medium text-ink shadow-card',
                      'transition-[transform,box-shadow] duration-[160ms] hover:-translate-y-px hover:shadow-card-hover',
                      (plain.isFetchingNextPage || lowconf.isFetchingNextPage) &&
                        'cursor-not-allowed opacity-60',
                    )}
                  >
                    <MaterialIcon name="expand_more" size={16} />
                    Load more
                    {visible < merged.items.length && (
                      <span className="font-mono text-[11px] text-muted tabular-nums">
                        {merged.items.length - visible} left
                      </span>
                    )}
                  </button>
                </div>
              )}
            </>
          )}
          <BackendNote />
        </>
      )}
      {result && <ReleaseToast result={result} onDismiss={() => setResult(null)} />}
      {pending &&
        (pending.action === 'match' ? (
          <HandMatchDialog
            release={pending.release}
            rows={series.data ?? []}
            onCancel={() => setPending(null)}
            onConfirm={(input) => void runAction(input)}
          />
        ) : (
          <ActionDialog
            action={pending.action}
            release={pending.release}
            onCancel={() => setPending(null)}
            onConfirm={(input) => void runAction(input)}
          />
        ))}
    </div>
  );
}

interface PendingAction {
  action: ReleaseActionId;
  release: ReleaseDetail;
}

interface ReleaseOutcome {
  kind: 'success' | 'error';
  title: string;
  detail?: string;
}

/**
 * Desktop has no top bar on this route, so the page carries its own
 * heading; below `md` the shell's mobile bar already names the page
 * and holds the icon-only refresh button.
 */
function ReleasesHeader({
  refreshedAt,
  onRefresh,
}: {
  refreshedAt: string;
  onRefresh: () => void;
}) {
  return (
    <header className="mb-5 hidden items-start justify-between gap-4 md:flex">
      <div className="flex min-w-0 flex-col gap-1">
        <h1 className="font-semibold text-[22px] text-ink tracking-[-0.4px]">Releases</h1>
        <p className="text-[13px] text-muted">
          Indexed releases the matcher gave up on or resolved without confidence.
        </p>
      </div>
      <button
        type="button"
        onClick={onRefresh}
        title={`counts as of ${refreshedAt}`}
        className={cn(
          'ml-auto inline-flex h-9 shrink-0 cursor-pointer items-center gap-2 self-start rounded-md border border-line-soft bg-surface px-3 text-sm font-medium text-ink shadow-card',
          'transition-[transform,box-shadow] duration-[160ms] ease-out hover:-translate-y-px hover:shadow-card-hover',
        )}
      >
        <MaterialIcon name="refresh" size={16} />
        Refresh
      </button>
    </header>
  );
}

function StatusPill({
  status,
  size = 'default',
}: {
  status: MatchStatus;
  size?: 'default' | 'compact';
}) {
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center gap-1.5 rounded-full border border-line-soft bg-paper font-mono text-ink-2 uppercase',
        size === 'compact'
          ? 'h-[18px] px-1.5 text-[9px] tracking-[0.3px]'
          : 'h-[22px] px-2.5 text-[10px] tracking-[0.5px]',
      )}
    >
      <span className={cn('h-1.5 w-1.5 shrink-0 rounded-full', STATUS_DOT[status])} />
      {STATUS_LABELS[status]}
    </span>
  );
}

/**
 * Confidence first, as a large tabular number in its own column — it
 * is the triage signal, and a fixed column keeps the numbers aligned
 * down the list. Absent confidence renders a dimmed `––`, never 0.
 */
function ConfidenceCell({ confidence }: { confidence: number | null }) {
  return (
    <span className="flex w-[52px] shrink-0 flex-col items-center justify-center self-stretch">
      <span
        className={cn(
          'font-semibold text-[19px] leading-none tracking-[-0.5px] tabular-nums',
          confidence == null && 'text-muted',
        )}
        style={{ color: confidenceColor(confidence) }}
      >
        {confidence == null ? (
          <span className="text-muted opacity-60">––</span>
        ) : (
          <>
            {Math.round(confidence * 100)}
            <span className="font-medium text-[12px]">%</span>
          </>
        )}
      </span>
    </span>
  );
}

/**
 * One release. Below `sm` the status pill folds into the meta line so
 * the title keeps the full row width and the fixed pill column is
 * dropped. Attempt count is deliberately absent — it is detail-level
 * trivia, not triage.
 */
function ReleaseRow({
  release,
  seriesTitle,
  onOpen,
}: {
  release: ReleaseItem;
  seriesTitle: string | undefined;
  onOpen: () => void;
}) {
  const size = release.sizeBytes == null ? null : formatBytesDecimal(release.sizeBytes);
  const low = isLowConfidence(release.matchStatus, release.confidence);
  return (
    <button
      type="button"
      onClick={onOpen}
      className="group flex w-full cursor-pointer items-center gap-3 border-line-soft border-b px-4 py-3 text-left outline-none transition-colors last:border-b-0 hover:bg-overlay-soft"
    >
      <ConfidenceCell confidence={release.confidence} />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <span className="truncate font-mono text-[12.5px] text-ink leading-snug">
          {release.title}
        </span>
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[10.5px] text-muted tabular-nums [&>span]:whitespace-nowrap">
          <span className="sm:hidden">
            <StatusPill status={release.matchStatus} size="compact" />
          </span>
          {seriesTitle && <span className="text-ink-2">{seriesTitle}</span>}
          <span>
            {formatStampUTC(release.publishedAt)} ({ageOf(release.publishedAt)} ago)
          </span>
          {size && <span>· {size}</span>}
        </div>
      </div>
      <span className="hidden w-[108px] shrink-0 flex-col items-start gap-1 sm:flex">
        <StatusPill status={release.matchStatus} />
        {low && (
          <span className="font-mono text-[9.5px] text-status-incomplete uppercase tracking-[0.5px]">
            low confidence
          </span>
        )}
      </span>
      <span className="shrink-0">
        <MaterialIcon name="chevron_right" size={18} className="text-muted" />
      </span>
    </button>
  );
}

function ageOf(iso: string): string {
  const at = Date.parse(iso);
  return formatCompactAge(Number.isNaN(at) ? 0 : Math.max(0, Date.now() - at));
}

/**
 * Multi-select status filter over all five states plus the client-side
 * "Low confidence" pseudo-entry and a reset back to the attention set.
 * Counts come from the queue-stats endpoint; low confidence has no
 * server count, so that row shows none rather than a fabricated one.
 */
function StatusFilter({
  active,
  counts,
  onToggle,
  onReset,
}: {
  active: ReadonlySet<ReleaseFilterKey>;
  counts: QueueStats | undefined;
  onToggle: (key: ReleaseFilterKey) => void;
  onReset: () => void;
}) {
  const value = summariseFilter(active);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label="Filter by status"
          className={cn(
            'relative inline-flex h-9 w-9 cursor-pointer items-center justify-center sm:w-auto sm:justify-start sm:gap-2 sm:px-3',
            'rounded-md border border-line-soft bg-surface text-sm text-ink shadow-card',
            'transition-[transform,box-shadow,background-color,color] duration-[160ms] ease-out',
            'hover:-translate-y-px hover:bg-overlay-soft hover:shadow-card-hover',
            'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-overlay',
          )}
        >
          <MaterialIcon name="flag" />
          <span className="hidden text-muted lg:inline">Status</span>
          <span className="hidden sm:inline">{value}</span>
          <ChevronDown aria-hidden="true" className="hidden h-3.5 w-3.5 text-muted sm:inline" />
          {active.size > 0 && (
            <span
              aria-hidden="true"
              className="absolute top-1 right-1 h-1.5 w-1.5 rounded-full bg-status-airing sm:hidden"
            />
          )}
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="min-w-[14rem]">
        <DropdownMenuLabel>Filter by status</DropdownMenuLabel>
        {MATCH_STATUSES.map((status) => (
          <DropdownMenuCheckboxItem
            key={status}
            checked={active.has(status)}
            onCheckedChange={() => onToggle(status)}
            onSelect={(e) => e.preventDefault()}
          >
            <span className="flex w-full items-center gap-2">
              <span
                aria-hidden="true"
                className={cn('h-2 w-2 shrink-0 rounded-full', STATUS_DOT[status])}
              />
              <span>{STATUS_LABELS[status]}</span>
              {counts && (
                <span className="ml-auto font-mono text-[11px] text-muted tabular-nums">
                  {counts[status]}
                </span>
              )}
            </span>
          </DropdownMenuCheckboxItem>
        ))}
        <DropdownMenuSeparator />
        <DropdownMenuCheckboxItem
          checked={active.has('lowconf')}
          onCheckedChange={() => onToggle('lowconf')}
          onSelect={(e) => e.preventDefault()}
        >
          <span className="flex w-full items-center gap-2">
            <span
              aria-hidden="true"
              className="h-2 w-2 shrink-0 rounded-full bg-status-incomplete"
            />
            <span>Low confidence</span>
          </span>
        </DropdownMenuCheckboxItem>
        <DropdownMenuItem className="pl-7" onSelect={onReset}>
          <span className="absolute left-2 inline-flex h-3.5 w-3.5 items-center justify-center text-muted">
            <MaterialIcon name="restart_alt" size={13} />
          </span>
          <span>Needs attention only</span>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function summariseFilter(active: ReadonlySet<ReleaseFilterKey>): string {
  if (active.size === 0) {
    return 'All';
  }
  if (active.size === 1) {
    const [only] = active;
    if (!only) {
      return 'All';
    }
    return only === 'lowconf' ? 'Low confidence' : STATUS_LABELS[only];
  }
  const attention =
    active.size === ATTENTION_FILTER.length && ATTENTION_FILTER.every((key) => active.has(key));
  return attention ? 'Needs attention' : `${active.size} selected`;
}

function Field({ label, children }: { label: string; children?: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <span className="font-mono text-[9.5px] text-muted uppercase tracking-[1px]">{label}</span>
      <span className="min-w-0 truncate font-mono text-[12px] text-ink-2">
        {children ?? <span className="text-muted">—</span>}
      </span>
    </div>
  );
}

/**
 * Full view rather than a sheet: the evidence lists are the reason to
 * open a release at all, and a sheet would put them behind a scroll
 * inside a scroll.
 */
function ReleaseDetailView({
  release,
  loading,
  error,
  seriesTitle,
  onBack,
  onAction,
}: {
  release: ReleaseDetail | null;
  loading: boolean;
  error: string | null;
  seriesTitle: string | undefined;
  onBack: () => void;
  onAction: (action: ReleaseActionId, release: ReleaseDetail) => void;
}) {
  const narrow = useNarrow();
  const back = (
    <button
      type="button"
      onClick={onBack}
      className="inline-flex h-8 cursor-pointer items-center gap-1.5 self-start rounded-md pr-2 font-medium text-[13px] text-muted transition-colors hover:text-ink"
    >
      <MaterialIcon name="arrow_back" size={16} />
      Attention queue
    </button>
  );

  if (!release) {
    return (
      <div className="flex flex-col gap-4">
        {back}
        {error && <ReleasesBlank icon="error" title="Could not load this release" sub={error} />}
        {loading && !error ? null : null}
      </div>
    );
  }

  const low = isLowConfidence(release.matchStatus, release.confidence);
  const actions = releaseActions(release.matchStatus, release.confidence);
  // The server orders match events oldest first; the operator reads
  // the newest decision first.
  const events = [...release.matchEvents].reverse();

  return (
    <div className="flex flex-col gap-4">
      {back}
      <header className="rounded-[14px] border border-line-soft bg-surface p-5 shadow-card">
        <div className="flex flex-col gap-3">
          <div className="flex items-start gap-3">
            <div className="flex min-w-0 flex-1 flex-col gap-1.5">
              <p className="font-mono text-[13px] text-ink leading-[1.5] [overflow-wrap:anywhere]">
                {release.title}
              </p>
              {seriesTitle && (
                <span className="font-mono text-[10.5px] text-muted">{seriesTitle}</span>
              )}
            </div>
            <StatusPill status={release.matchStatus} />
          </div>
          <span className="font-mono text-[10.5px] text-muted [overflow-wrap:anywhere]">
            {release.infohash}
          </span>
        </div>
        <div className="mt-5 grid grid-cols-2 gap-x-6 gap-y-4 border-line-soft border-t pt-5 sm:grid-cols-4">
          <Field label="Sources">{release.sources.join(', ')}</Field>
          <Field label="Published">{formatStampUTC(release.publishedAt)}</Field>
          <Field label="Attempts">{String(release.attemptCount)}</Field>
          <Field label="Size">
            {release.sizeBytes == null ? undefined : formatBytesDecimal(release.sizeBytes)}
          </Field>
          <Field label="Confidence">
            {release.confidence == null ? (
              <span className="text-muted">N/A</span>
            ) : (
              <span style={{ color: confidenceColor(release.confidence) }}>
                {confidencePercent(release.confidence)}
              </span>
            )}
          </Field>
          <Field label="Ref">{release.ref ?? undefined}</Field>
        </div>
        {actions.length > 0 && (
          <div className="mt-5 flex flex-wrap gap-2 border-line-soft border-t pt-5">
            {actions.map((action) => (
              <button
                key={action.id}
                type="button"
                onClick={() => onAction(action.id, release)}
                className={cn(
                  'inline-flex h-9 cursor-pointer items-center gap-2 rounded-md px-3.5 font-medium text-sm shadow-card',
                  'transition-[transform,box-shadow,background-color,color] duration-[160ms] ease-out hover:-translate-y-px hover:shadow-card-hover',
                  action.primary
                    ? 'bg-ink text-paper'
                    : 'border border-line-soft bg-surface text-ink',
                )}
              >
                <MaterialIcon name={action.icon} size={16} />
                {action.label}
              </button>
            ))}
          </div>
        )}
        {low && (
          <p className="mt-3 text-[12px] text-muted leading-[1.55]">
            Auto-matched below the {Math.round(LOW_CONFIDENCE * 100)}% threshold. Affirming records
            the same ref as an operator match, which is certain by definition; correcting resolves a
            different series.
          </p>
        )}
        {release.matchStatus === 'suppressed' && (
          <p className="mt-3 text-[12px] text-muted leading-[1.55]">
            Suppressed releases stay out of the claim queue until an operator matches them to a ref
            — there is no unsuppress.
          </p>
        )}
        {release.matchStatus === 'dead' && (
          <p className="mt-5 border-line-soft border-t pt-5 text-[12px] text-muted">
            Dead is terminal, and set by the downloader when a torrent is proven dead — not an
            operator transition.
          </p>
        )}
      </header>
      <div className="grid gap-4 lg:grid-cols-2">
        <Section title="Source postings" count={release.rawItems.length} collapsible={narrow}>
          <ul>
            {release.rawItems.map((item) => (
              <Posting key={item.id} posting={item} />
            ))}
          </ul>
        </Section>
        <Section title="Match events" count={release.matchEvents.length} collapsible={narrow}>
          <ul>
            {events.map((event) => (
              <EventRow key={event.id} event={event} />
            ))}
          </ul>
        </Section>
      </div>
    </div>
  );
}

function Posting({ posting }: { posting: RawItem }) {
  return (
    <li className="flex flex-col gap-1 border-line-soft border-b px-4 py-3 last:border-b-0">
      <span className="font-mono text-[11.5px] text-ink-2 leading-snug [overflow-wrap:anywhere]">
        {posting.title}
      </span>
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[10.5px] text-muted tabular-nums [&>span]:whitespace-nowrap">
        <span className="text-ink-2">{posting.source}</span>
        <span>· {formatStampUTC(posting.publishedAt)}</span>
      </div>
      {posting.url && (
        <a
          href={posting.url}
          target="_blank"
          rel="noreferrer"
          className="truncate font-mono text-[10.5px]"
        >
          {posting.url}
        </a>
      )}
    </li>
  );
}

function EventRow({ event }: { event: MatchEvent }) {
  return (
    <li className="flex flex-col gap-1.5 border-line-soft border-b px-4 py-3 last:border-b-0">
      <div className="flex flex-wrap items-center gap-2">
        <StatusPill status={event.status} size="compact" />
        <span className="font-mono text-[10.5px] text-muted tabular-nums">
          {formatStampUTC(event.createdAt)}
        </span>
        {event.confidence != null && (
          <span
            className="font-mono text-[10.5px] tabular-nums"
            style={{ color: confidenceColor(event.confidence) }}
          >
            conf {confidencePercent(event.confidence)}
          </span>
        )}
        {event.ref && <span className="font-mono text-[10.5px] text-ink-2">· {event.ref}</span>}
      </div>
      {event.reason && (
        <span className="text-[11.5px] text-muted leading-[1.5] [overflow-wrap:anywhere]">
          {event.reason}
        </span>
      )}
    </li>
  );
}

/**
 * Evidence list. Collapsible (and collapsed) on mobile so the actions
 * stay above the fold; always open at desktop width, where the two
 * lists sit side by side.
 */
function Section({
  title,
  count,
  collapsible,
  children,
}: {
  title: string;
  count: number;
  collapsible: boolean;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(!collapsible);
  useEffect(() => {
    setOpen(!collapsible);
  }, [collapsible]);
  const head = (
    <>
      <span className="font-semibold text-[12.5px] text-ink tracking-[-0.1px]">{title}</span>
      <span className="font-mono text-[11px] text-muted tabular-nums">{count}</span>
      {collapsible && (
        <MaterialIcon
          name={open ? 'expand_less' : 'expand_more'}
          size={16}
          className="ml-auto shrink-0 text-muted"
        />
      )}
    </>
  );
  return (
    <section className="overflow-hidden rounded-[12px] border border-line-soft bg-surface shadow-card">
      {collapsible ? (
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          className={cn(
            'flex w-full cursor-pointer items-center gap-2 bg-surface-2 px-4 py-3 text-left',
            open && 'border-line-soft border-b',
          )}
        >
          {head}
        </button>
      ) : (
        <div className="flex items-center gap-2 border-line-soft border-b bg-surface-2 px-4 py-2.5">
          {head}
        </div>
      )}
      {open && children}
    </section>
  );
}

/**
 * `true` below the `sm` breakpoint. The evidence lists' collapsed-by-
 * default state is a JS decision (CSS can express the layout but not
 * "starts closed here, open there" behind one toggle).
 */
function useNarrow(): boolean {
  const [narrow, setNarrow] = useState(
    () => typeof window !== 'undefined' && window.matchMedia('(max-width: 640px)').matches,
  );
  useEffect(() => {
    const mq = window.matchMedia('(max-width: 640px)');
    const onChange = () => setNarrow(mq.matches);
    onChange();
    mq.addEventListener('change', onChange);
    return () => mq.removeEventListener('change', onChange);
  }, []);
  return narrow;
}

function PosterThumb({ title, className }: { title: string; className: string }) {
  return (
    <span
      className={cn(
        'relative block shrink-0 overflow-hidden rounded-[4px] bg-surface shadow-poster',
        className,
      )}
      style={{ aspectRatio: '0.7' }}
    >
      <PosterArt title={title} />
    </span>
  );
}

/**
 * Hand match: search the loaded library (the same fuzzy search and the
 * same posters the library grid uses, so a candidate reads as the
 * series the operator knows), confirm the pick through the library
 * manager's resolve, then submit the canonical ref to the indexer.
 * Resolve failures pass through verbatim — the SPA never guesses a
 * series.
 *
 * Modal at desktop, bottom sheet below `sm`.
 */
function HandMatchDialog({
  release,
  rows,
  onCancel,
  onConfirm,
}: {
  release: ReleaseDetail;
  rows: readonly ListRow[];
  onCancel: () => void;
  onConfirm: (input: { ref: string; reason: string }) => void;
}) {
  const [query, setQuery] = useState('');
  const [resolved, setResolved] = useState<ResolvedSeries | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [reason, setReason] = useState('');
  const resolve = useResolveSeries();

  const marker = useMemo<EpisodeMarker | null>(
    () => parseEpisodeMarker(release.title),
    [release.title],
  );

  const trimmed = query.trim();
  const candidates = useMemo(
    () => (trimmed ? searchLibrary(rows, trimmed).slice(0, CANDIDATE_LIMIT) : []),
    [rows, trimmed],
  );

  const ref = resolved ? composeReleaseRef(resolved.ref, marker) : '';

  async function pick(row: ListRow) {
    if (!row.ref) {
      return;
    }
    setError(null);
    try {
      const resolution = await resolve.mutateAsync([row.ref]);
      const candidate = resolution.candidates[0];
      if (!candidate || resolution.candidates.length > 1) {
        // Cardinality is the resolve contract's outcome encoding: no
        // candidate means the provider does not know this ref, more
        // than one means the term was ambiguous. Neither is something
        // the SPA may pick for the operator.
        setError(
          candidate
            ? `resolve returned ${resolution.candidates.length} candidates for "${row.ref}" — ambiguous, pick a more specific series`
            : `resolve returned no candidates for "${row.ref}"`,
        );
        return;
      }
      // `episodes` comes from the library row rather than the provider
      // candidate, which carries no episode total.
      setResolved({
        ref: candidate.ref,
        title: candidate.preferredTitle,
        alt: candidate.canonicalTitle,
        year: candidate.year,
        episodes: row.episodeCount,
      });
    } catch (err) {
      setError(apiErrorMessage(err, 'resolve failed'));
    }
  }

  return (
    <DialogPrimitive.Root open onOpenChange={(next) => !next && onCancel()}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-[80] bg-overlay backdrop-blur-[2px]" />
        <div className="pointer-events-none fixed inset-0 z-[81] flex items-end justify-center sm:items-center sm:p-5">
          <DialogPrimitive.Content
            aria-describedby={undefined}
            className={cn(
              'pointer-events-auto flex max-h-[92dvh] w-full flex-col overflow-hidden',
              'rounded-t-[16px] bg-surface shadow-pop outline-none',
              'sm:max-h-[88dvh] sm:w-[460px] sm:max-w-[92vw] sm:rounded-[14px]',
            )}
            style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
          >
            <div className="flex shrink-0 items-center gap-2.5 px-5 pt-5">
              <span className="inline-flex h-8 w-8 items-center justify-center rounded-md bg-overlay-soft text-ink">
                <MaterialIcon name="link" size={18} />
              </span>
              <DialogPrimitive.Title className="font-semibold text-[15px] text-ink tracking-[-0.2px]">
                Hand match release
              </DialogPrimitive.Title>
              <DialogPrimitive.Close asChild>
                <GhostIconButton size="lg" aria-label="Close" className="ml-auto sm:hidden">
                  <MaterialIcon name="close" size={18} />
                </GhostIconButton>
              </DialogPrimitive.Close>
            </div>
            <p
              className="shrink-0 truncate px-5 pt-3 font-mono text-[11px] text-ink-2"
              title={release.title}
            >
              {release.title}
            </p>
            {resolved ? (
              <div className="min-h-0 flex-1 overflow-y-auto px-5 pt-3">
                <div className="rounded-[8px] border border-line-soft bg-paper px-3 py-2.5">
                  <div className="flex items-start gap-2.5">
                    <PosterThumb title={resolved.title} className="w-[52px]" />
                    <MaterialIcon
                      name="check_circle"
                      size={15}
                      className="mt-px shrink-0 text-status-complete"
                    />
                    <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                      <span className="truncate font-medium text-[13px] text-ink">
                        {resolved.title}
                      </span>
                      <span className="font-mono text-[10.5px] text-muted leading-[1.5]">
                        {[resolved.alt, resolved.year, `${resolved.episodes} episodes`]
                          .filter(Boolean)
                          .join(' · ')}
                      </span>
                    </div>
                    <button
                      type="button"
                      onClick={() => setResolved(null)}
                      className="shrink-0 cursor-pointer font-medium text-[12px] text-muted transition-colors hover:text-ink"
                    >
                      Change
                    </button>
                  </div>
                  <div className="mt-2.5 flex items-baseline gap-2 border-line-soft border-t pt-2.5">
                    <span className="font-mono text-[9.5px] text-muted uppercase tracking-[1px]">
                      Ref
                    </span>
                    <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-ink">
                      {ref}
                    </span>
                  </div>
                  <p className="mt-1 font-mono text-[10px] text-muted leading-[1.5]">
                    {marker
                      ? `episode segment ${ref.slice(ref.indexOf('#') + 1)} parsed from the release title`
                      : 'series-level ref — no episode parsed from the release title'}
                  </p>
                </div>
                <label className="mt-3 flex flex-col gap-1.5">
                  <span className="font-mono text-[9.5px] text-muted uppercase tracking-[1px]">
                    Reason <span className="text-muted/70">optional</span>
                  </span>
                  <input
                    value={reason}
                    onChange={(e) => setReason(e.target.value)}
                    placeholder="recorded to match_events"
                    spellCheck="false"
                    className="h-9 rounded-md border border-line-soft bg-paper px-2.5 text-[12.5px] text-ink outline-none transition-colors placeholder:text-muted focus:border-ink"
                  />
                </label>
              </div>
            ) : (
              <>
                <div className="shrink-0 px-5 pt-3">
                  <SearchField
                    className="w-full"
                    value={query}
                    hideShortcutHint
                    // The dialog opens for this search; anything else
                    // would need a second interaction to start.
                    autoFocus
                    placeholder="Search series — title or alias…"
                    onChange={(e) => {
                      setQuery(e.target.value);
                      setError(null);
                    }}
                    onClear={() => setQuery('')}
                  />
                </div>
                <div className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden px-5 pt-3">
                  {error && (
                    <p className="rounded-[8px] border border-status-error/40 bg-status-error/5 px-3 py-2.5 font-mono text-[11px] text-status-error leading-[1.5] [overflow-wrap:anywhere]">
                      {error}
                    </p>
                  )}
                  {!trimmed && !error && (
                    <p className="py-6 text-center text-[12.5px] text-muted leading-[1.5]">
                      Search the library for the series this release belongs to.
                    </p>
                  )}
                  {trimmed && candidates.length === 0 && !error && (
                    <p className="rounded-[8px] border border-line-soft bg-paper px-3 py-2.5 font-mono text-[11px] text-muted leading-[1.5] [overflow-wrap:anywhere]">
                      no series in the library matches "{trimmed}"
                    </p>
                  )}
                  {candidates.length > 0 && (
                    <ul className="overflow-hidden rounded-[8px] border border-line-soft">
                      {candidates.map((row) => (
                        <li key={row.ref ?? row.title}>
                          <button
                            type="button"
                            disabled={!row.ref || resolve.isPending}
                            onClick={() => void pick(row)}
                            className={cn(
                              'flex w-full cursor-pointer items-center gap-3 border-line-soft border-b bg-surface px-3 py-2.5 text-left transition-colors last:border-b-0 hover:bg-overlay-soft',
                              (!row.ref || resolve.isPending) && 'cursor-not-allowed opacity-60',
                            )}
                          >
                            <PosterThumb title={row.title} className="w-[42px]" />
                            <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                              <span className="truncate font-medium text-[13px] text-ink">
                                {row.title}
                              </span>
                              <span className="font-mono text-[10.5px] text-muted leading-[1.5]">
                                {[row.canonicalTitle, `${row.episodeCount} eps`]
                                  .filter(Boolean)
                                  .join(' · ')}
                              </span>
                            </div>
                            <span className="shrink-0 font-mono text-[10.5px] text-muted">
                              {row.ref}
                            </span>
                          </button>
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
              </>
            )}
            <div className="flex shrink-0 justify-end gap-2 px-5 pt-4 pb-5">
              <button
                type="button"
                onClick={onCancel}
                className="inline-flex h-9 cursor-pointer items-center rounded-md px-3 font-medium text-sm text-muted transition-colors hover:bg-overlay-soft hover:text-ink"
              >
                Cancel
              </button>
              <button
                type="button"
                disabled={!resolved}
                onClick={() => resolved && onConfirm({ ref, reason: reason.trim() })}
                className={cn(
                  'inline-flex h-9 items-center gap-2 rounded-md px-3.5 font-medium text-sm shadow-card transition-[transform,box-shadow] duration-[160ms]',
                  resolved
                    ? 'cursor-pointer bg-ink text-paper hover:-translate-y-px hover:shadow-card-hover'
                    : 'cursor-not-allowed bg-paper text-muted opacity-60',
                )}
              >
                <MaterialIcon name="link" size={16} />
                Match release
              </button>
            </div>
          </DialogPrimitive.Content>
        </div>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

interface ResolvedSeries {
  ref: string;
  title: string;
  alt?: string;
  year?: number;
  episodes: number;
}

const ACTION_COPY: Record<'affirm' | 'requeue', { title: string; icon: string; cta: string }> = {
  affirm: { title: 'Affirm match', icon: 'verified', cta: 'Affirm match' },
  requeue: { title: 'Requeue release', icon: 'restart_alt', cta: 'Requeue' },
};

/**
 * Confirm gate for the two ref-less actions. Every transition confirms
 * before mutating and takes an optional reason recorded to
 * match_events.
 */
function ActionDialog({
  action,
  release,
  onCancel,
  onConfirm,
}: {
  action: ReleaseActionId;
  release: ReleaseDetail;
  onCancel: () => void;
  onConfirm: (input: { reason: string }) => void;
}) {
  const [reason, setReason] = useState('');
  if (action === 'match') {
    return null;
  }
  const copy = ACTION_COPY[action];
  const body =
    action === 'affirm'
      ? 'Keeps the current ref and records it as an operator match. Operator matches are certain, so the release leaves the low-confidence set at 100%.'
      : `Returns this release to unmatched and clears its exhaustion accounting (${release.attemptCount} attempts), so the claim queue offers it again.`;
  return (
    <DialogPrimitive.Root open onOpenChange={(next) => !next && onCancel()}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-[80] bg-overlay backdrop-blur-[2px]" />
        <div className="pointer-events-none fixed inset-0 z-[81] grid place-items-center px-5">
          <DialogPrimitive.Content
            aria-describedby={undefined}
            className="pointer-events-auto relative max-h-full w-full max-w-[460px] overflow-y-auto rounded-[14px] bg-surface p-5 shadow-pop outline-none"
          >
            <div className="flex items-center gap-2.5">
              <span className="inline-flex h-8 w-8 items-center justify-center rounded-md bg-overlay-soft text-ink">
                <MaterialIcon name={copy.icon} size={18} />
              </span>
              <DialogPrimitive.Title className="font-semibold text-[15px] text-ink tracking-[-0.2px]">
                {copy.title}
              </DialogPrimitive.Title>
            </div>
            <p className="mt-3 text-[13px] text-ink-2 leading-[1.55]">{body}</p>
            <div className="mt-3 flex flex-col gap-1 rounded-[8px] bg-paper px-3 py-2.5">
              <span className="truncate font-mono text-[11px] text-ink-2" title={release.title}>
                {release.title}
              </span>
              {action === 'affirm' && (
                <span className="font-mono text-[11px] text-ink">
                  {release.ref}{' '}
                  <span className="text-muted">
                    · {confidencePercent(release.confidence)} → 100%
                  </span>
                </span>
              )}
            </div>
            <label className="mt-3 flex flex-col gap-1.5">
              <span className="font-mono text-[9.5px] text-muted uppercase tracking-[1px]">
                Reason <span className="text-muted/70">optional</span>
              </span>
              <input
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                placeholder="recorded to match_events"
                spellCheck="false"
                // biome-ignore lint/a11y/noAutofocus: the dialog exists to take this one optional input; focusing it is the point.
                autoFocus
                className="h-9 rounded-md border border-line-soft bg-paper px-2.5 text-[12.5px] text-ink outline-none transition-colors placeholder:text-muted focus:border-ink"
              />
            </label>
            <div className="mt-4 flex justify-end gap-2">
              <button
                type="button"
                onClick={onCancel}
                className="inline-flex h-9 cursor-pointer items-center rounded-md px-3 font-medium text-sm text-muted transition-colors hover:bg-overlay-soft hover:text-ink"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => onConfirm({ reason: reason.trim() })}
                className={cn(
                  'inline-flex h-9 cursor-pointer items-center gap-2 rounded-md bg-ink px-3.5 font-medium text-sm text-paper shadow-card',
                  'transition-[transform,box-shadow] duration-[160ms] hover:-translate-y-px hover:shadow-card-hover',
                )}
              >
                <MaterialIcon name={copy.icon} size={16} />
                {copy.cta}
              </button>
            </div>
          </DialogPrimitive.Content>
        </div>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

const TOAST_TTL_MS = 6000;

/**
 * Fixed layer, same position from every view and over dialogs:
 * bottom-right at `sm`+, full-width bottom-centre below. Success
 * self-expires after six seconds with the shared drain bar; errors
 * stay until dismissed — they name a transition the operator has to
 * read.
 */
function ReleaseToast({ result, onDismiss }: { result: ReleaseOutcome; onDismiss: () => void }) {
  const isError = result.kind === 'error';
  // `result` is deliberately a dependency: a second action replacing
  // the toast restarts the countdown rather than inheriting the
  // first one's remaining time.
  // biome-ignore lint/correctness/useExhaustiveDependencies: see above — the timer must restart when the content is swapped.
  useEffect(() => {
    if (isError) {
      return;
    }
    const timer = setTimeout(onDismiss, TOAST_TTL_MS);
    return () => clearTimeout(timer);
  }, [isError, onDismiss, result]);

  return (
    <div
      role="status"
      aria-live="polite"
      className="pointer-events-none fixed inset-x-4 bottom-4 z-[90] flex justify-center sm:inset-x-auto sm:right-5 sm:bottom-5 sm:justify-end"
      style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
    >
      <div className="kura-toast-in pointer-events-auto relative flex w-full items-start gap-2.5 overflow-hidden rounded-[10px] border border-line-soft bg-surface px-4 py-3 shadow-pop sm:w-[380px]">
        <MaterialIcon
          name={isError ? 'error' : 'check_circle'}
          size={16}
          className={cn('mt-px shrink-0', isError ? 'text-status-error' : 'text-status-complete')}
        />
        <div className="flex min-w-0 flex-1 flex-col gap-1.5">
          <p className="font-medium text-[13px] text-ink">{result.title}</p>
          {result.detail && (
            <p className="font-mono text-[11px] text-muted leading-[1.5] [overflow-wrap:anywhere]">
              {result.detail}
            </p>
          )}
        </div>
        <button
          type="button"
          onClick={onDismiss}
          aria-label="Dismiss"
          className="shrink-0 cursor-pointer text-muted transition-colors hover:text-ink"
        >
          <MaterialIcon name="close" size={15} />
        </button>
        {!isError && (
          <span aria-hidden="true" className="absolute inset-x-0 bottom-0 h-[2px] bg-line-soft">
            <span className="kura-toast-bar block h-full origin-left bg-status-complete/70" />
          </span>
        )}
      </div>
    </div>
  );
}

function ReleasesBlank({
  icon,
  title,
  sub,
  children,
}: {
  icon: string;
  title: string;
  sub: string;
  children?: ReactNode;
}) {
  return (
    <div className="grid place-items-center py-20">
      <div className="flex max-w-sm flex-col items-center gap-3 text-center">
        <MaterialIcon name={icon} size={30} className="text-muted" />
        <h2 className="font-semibold text-sm text-ink tracking-tight">{title}</h2>
        <p className="text-[13px] text-muted leading-[1.5]">{sub}</p>
        {children}
      </div>
    </div>
  );
}

/**
 * Collapsed strip at the foot of the list: the indexer-side deltas
 * this page depends on, kept beside the surface so the two stay
 * visible together.
 */
function BackendNote() {
  const [open, setOpen] = useState(false);
  return (
    <div className="mt-6 rounded-[12px] border border-line border-dashed px-4 py-3">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex w-full cursor-pointer items-center gap-2 text-left"
      >
        <MaterialIcon name="api" size={15} className="shrink-0 text-muted" />
        <span className="font-medium text-[12.5px] text-ink-2">
          Backend deltas this page depends on
        </span>
        <MaterialIcon
          name={open ? 'expand_less' : 'expand_more'}
          size={16}
          className="ml-auto shrink-0 text-muted"
        />
      </button>
      {open && (
        <div className="mt-3 flex flex-col gap-3 border-line-soft border-t pt-3">
          <ul className="flex flex-col gap-2 font-mono text-[11px] text-muted leading-[1.6]">
            <li>
              <span className="text-ink-2">GET /releases</span> gains a{' '}
              <span className="text-ink-2">status</span> filter — today it is hard-scoped to
              matched.
            </li>
            <li>
              Transition table gains operator rows:{' '}
              <span className="text-ink-2">suppressed|exhausted → matched</span> (with ref) and{' '}
              <span className="text-ink-2">exhausted → unmatched</span>; requeue resets exhaustion
              accounting.
            </li>
            <li>
              <span className="text-ink-2">SetStatusRequest</span> gains an optional{' '}
              <span className="text-ink-2">ref</span> for the hand-match case.
            </li>
            <li>
              Transitions stay idempotent and audit through{' '}
              <span className="text-ink-2">match_events</span> — no new tables.{' '}
              <span className="text-ink-2">dead</span> stays terminal and downloader-owned; queue
              stats endpoint is already sufficient.
            </li>
            <li>
              No library-manager changes — <span className="text-ink-2">resolve</span> already
              exists.
            </li>
          </ul>
          <div className="flex flex-col gap-2 rounded-[8px] bg-paper px-3 py-2.5">
            <span className="font-mono text-[9.5px] text-muted uppercase tracking-[1px]">
              Composition
            </span>
            <p className="font-mono text-[11px] text-muted leading-[1.6]">
              The gateway's single origin is the composition point: this page calls both leaf APIs
              directly —<span className="text-ink-2"> /api/releases/v1/*</span> for the queue and
              its transitions,
              <span className="text-ink-2"> /api/library/v1/series/resolve</span> for the series
              lookup. No gateway bridge or aggregation endpoints, and no indexer↔library-manager
              communication.
            </p>
          </div>
        </div>
      )}
    </div>
  );
}
