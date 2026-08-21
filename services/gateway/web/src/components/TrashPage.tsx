import * as DialogPrimitive from '@radix-ui/react-dialog';
import { ChevronDown } from 'lucide-react';
import { type ReactNode, useCallback, useEffect, useState } from 'react';
import { apiErrorMessage } from '@/api/client';
import { useEmptyTrash, useTrashList } from '@/api/hooks';
import type { TrashEntry, TrashSeriesEntry } from '@/api/types.gen';
import { ResolutionChip, SourceChip } from '@/components/series/QualityChip';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { GhostIconButton } from '@/components/ui/ghost-icon-btn';
import { MaterialIcon } from '@/components/ui/material-icon';
import { cn } from '@/lib/cn';
import { shortMarker } from '@/lib/format';
import { parseMediaPath } from '@/lib/mediaPath';
import {
  filterTrashByAge,
  formatTrashAge,
  formatTrashBytes,
  formatTrashStamp,
  nextTrashSort,
  olderThanParam,
  sortTrashGroups,
  TRASH_AGE_OPTIONS,
  type TrashOutcome,
  type TrashSortKey,
  trashAgeLabel,
  trashEntryAge,
  trashEntryBytes,
  trashGroupBytes,
  trashOldestAge,
  trashOutcome,
  trashTotalBytes,
  trashTotalEntries,
} from '@/lib/trash';
import { useTrashView } from '@/state/trashView';

const SORT_LABELS: Record<TrashSortKey, string> = {
  size: 'Size',
  trashed: 'Trashed',
  title: 'Title',
};
const SORT_ORDER: readonly TrashSortKey[] = ['size', 'trashed', 'title'];
const DIRECTION_GLYPH = { asc: '▲', desc: '▼' } as const;

/** How long a non-blocking result banner stays up before it drains away. */
const BANNER_TTL_MS = 6000;

/**
 * What an Empty action will act on: the whole listing or one series.
 * `groups` is the age-scoped client-side preview; the server re-derives
 * the same set from the `olderThan` the mutation sends.
 */
interface EmptyTarget {
  scope: 'all' | 'series';
  groups: TrashSeriesEntry[];
}

/**
 * Trash — replaced and removed media held on disk until emptied.
 *
 * One row per series rather than per file: a library can hold trash for
 * dozens of series, and the per-file table is only interesting once the
 * user has picked one. Opening a row reveals the entries.
 *
 * There is no search field. The listing is short, the age and sort
 * pills do the narrowing, and a query that filtered the view without
 * scoping the destructive action would misrepresent what Empty deletes.
 *
 * There is no restore action either — restoring puts files back at
 * recorded paths and can fail on conflicts, so it stays in the CLI.
 */
export function TrashPage() {
  const listing = useTrashList();
  const empty = useEmptyTrash();
  const maxAgeHours = useTrashView((s) => s.maxAgeHours);
  const setMaxAgeHours = useTrashView((s) => s.setMaxAgeHours);
  const sort = useTrashView((s) => s.sort);
  const setSort = useTrashView((s) => s.setSort);

  const [confirm, setConfirm] = useState<EmptyTarget | null>(null);
  const [openDirectory, setOpenDirectory] = useState<string | null>(null);
  const [result, setResult] = useState<TrashOutcome | null>(null);
  // Stable identity so the banner's self-expire timer isn't restarted
  // by unrelated re-renders (sort flips, dropdown opens).
  const dismissResult = useCallback(() => setResult(null), []);

  // One clock for the whole render pass, so the header rollup, the row
  // ages and the age filter can't disagree by a few milliseconds.
  const now = Date.now();
  const live = listing.data?.series ?? [];
  // Not memoized: the listing is per-series rollups (tens of entries at
  // personal-library scale), so filtering and sorting it costs less
  // than the dependency bookkeeping — and a memo keyed on anything but
  // `now` would go stale exactly when the age filter matters.
  const groups = sortTrashGroups(filterTrashByAge(live, maxAgeHours, now), sort);

  const ageLabel = trashAgeLabel(maxAgeHours).toLowerCase();
  const olderThan = olderThanParam(maxAgeHours);
  const open = groups.find((g) => g.directory === openDirectory);

  function scopedGroups(directory?: string): TrashSeriesEntry[] {
    return directory ? groups.filter((g) => g.directory === directory) : groups;
  }

  async function runEmpty(target: EmptyTarget) {
    setConfirm(null);
    setOpenDirectory(null);
    const ref = target.scope === 'series' ? target.groups[0]?.ref : undefined;
    try {
      setResult(trashOutcome(await empty.mutateAsync({ ref, olderThan })));
    } catch (err) {
      // The per-series route answers a failure with an HTTP error
      // rather than a `failures` array, so the envelope message is
      // reshaped into the same partial-outcome the banner renders.
      setResult({
        kind: 'partial',
        entries: 0,
        bytes: 0,
        failures: target.groups.map((g) => ({
          directory: g.directory,
          error: apiErrorMessage(err, 'empty failed'),
        })),
      });
    }
  }

  return (
    <div className="mx-auto w-full max-w-[1000px] px-4 pt-4 pb-8 sm:px-6 sm:pt-5 sm:pb-10">
      <TrashHeader
        groups={live}
        now={now}
        loading={listing.isPending}
        onEmptyAll={() => setConfirm({ scope: 'all', groups: scopedGroups() })}
      />

      {live.length > 0 && (
        <div className="mt-5 mb-4 flex flex-wrap items-center gap-2">
          <TriggerPill
            icon="schedule"
            label="Older than"
            value={trashAgeLabel(maxAgeHours)}
            ariaLabel="Filter by age"
            marked={maxAgeHours > 0}
            menuLabel="Older than"
          >
            {TRASH_AGE_OPTIONS.map((option) => (
              <DropdownMenuItem
                key={option.hours}
                className="pl-7"
                onSelect={() => setMaxAgeHours(option.hours)}
              >
                <span className="absolute left-2 inline-flex h-3.5 w-3.5 items-center justify-center text-status-airing">
                  {maxAgeHours === option.hours ? '✓' : null}
                </span>
                <span>{option.label}</span>
              </DropdownMenuItem>
            ))}
          </TriggerPill>
          <TriggerPill
            icon="sort"
            label="Sort"
            value={`${SORT_LABELS[sort.key]} ${DIRECTION_GLYPH[sort.direction]}`}
            ariaLabel="Sort"
            menuLabel="Sort by"
          >
            {SORT_ORDER.map((key) => (
              <DropdownMenuItem
                key={key}
                className="pl-7"
                onSelect={() => setSort(nextTrashSort(sort, key))}
              >
                <span className="absolute left-2 inline-flex h-3.5 w-3.5 items-center justify-center text-status-airing">
                  {sort.key === key ? DIRECTION_GLYPH[sort.direction] : null}
                </span>
                <span>{SORT_LABELS[key]}</span>
              </DropdownMenuItem>
            ))}
          </TriggerPill>
          <span className="ml-1 text-[13px] text-muted">
            {groups.length} series · {formatTrashBytes(trashTotalBytes(groups))}
            {maxAgeHours > 0 && ` older than ${ageLabel}`}
          </span>
        </div>
      )}

      {result && (
        <ResultBanner
          result={result}
          scopeNote={
            maxAgeHours > 0 ? `Scoped to entries older than ${ageLabel}.` : 'Scoped to any age.'
          }
          topGap={live.length === 0}
          onDismiss={dismissResult}
        />
      )}

      {listing.isPending ? null : live.length === 0 ? (
        <TrashBlank
          icon="delete"
          title="Trash is empty"
          sub="Files replaced or removed by a scan are held here before deletion. Nothing is waiting right now."
        />
      ) : groups.length === 0 ? (
        <TrashBlank
          icon="filter_alt_off"
          title="No entries match"
          sub={`Nothing in trash is older than ${ageLabel}.`}
        >
          <button
            type="button"
            onClick={() => setMaxAgeHours(0)}
            className={cn(
              'inline-flex h-9 cursor-pointer items-center gap-2 rounded-md border border-line-soft bg-surface px-3 text-sm font-medium text-ink shadow-card',
              'transition-[transform,box-shadow] duration-[160ms] ease-out hover:-translate-y-px hover:shadow-card-hover',
            )}
          >
            <MaterialIcon name="filter_alt_off" size={16} />
            Clear age filter
          </button>
        </TrashBlank>
      ) : (
        <div className="overflow-hidden rounded-[12px] border border-line-soft bg-surface shadow-card">
          {groups.map((group) => (
            <SeriesRow
              key={group.directory}
              group={group}
              now={now}
              onOpen={() => setOpenDirectory(group.directory)}
              onEmpty={() => setConfirm({ scope: 'series', groups: scopedGroups(group.directory) })}
            />
          ))}
        </div>
      )}

      <SeriesDetail
        group={open}
        now={now}
        onClose={() => setOpenDirectory(null)}
        onEmpty={() =>
          open && setConfirm({ scope: 'series', groups: scopedGroups(open.directory) })
        }
      />
      <EmptyConfirm
        target={confirm}
        ageLabel={maxAgeHours > 0 ? ageLabel : 'any age'}
        busy={empty.isPending}
        onCancel={() => setConfirm(null)}
        onConfirm={() => confirm && void runEmpty(confirm)}
      />
    </div>
  );
}

/**
 * Rollups + the page-level action. Size, not entry count, is the
 * headline: it is what decides whether emptying is worth doing. Oldest
 * age is the "is it time" signal beside it.
 */
function TrashHeader({
  groups,
  now,
  loading,
  onEmptyAll,
}: {
  groups: readonly TrashSeriesEntry[];
  now: number;
  loading: boolean;
  onEmptyAll: () => void;
}) {
  const entries = trashTotalEntries(groups);
  const filled = entries > 0;
  return (
    <header className="rounded-[14px] border border-line-soft bg-surface p-5 shadow-card">
      <div className="flex flex-col gap-5 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex min-w-0 flex-col gap-1">
          {/* Below md the PageMobileBar already titles the page. */}
          <h1 className="hidden font-semibold text-[22px] text-ink tracking-[-0.4px] md:block">
            Trash
          </h1>
          <p className="text-[13px] text-muted">Replaced and removed media, held until emptied.</p>
        </div>
        <button
          type="button"
          onClick={onEmptyAll}
          disabled={!filled || loading}
          className={cn(
            'inline-flex h-9 shrink-0 items-center gap-2 self-start rounded-md border px-3 text-sm font-medium shadow-card',
            'transition-[transform,box-shadow,background-color] duration-[160ms] ease-out',
            !filled || loading
              ? 'cursor-not-allowed border-line-soft bg-paper text-muted opacity-60'
              : 'cursor-pointer border-status-error/40 bg-surface text-status-error hover:-translate-y-px hover:bg-status-error hover:text-status-error-fg hover:shadow-card-hover',
          )}
        >
          <MaterialIcon name="delete_forever" size={16} />
          Empty trash
        </button>
      </div>
      {/* Empty stats render an em dash rather than a computed "0 B" /
          "1m", which would read as a measurement of nothing. */}
      <div className="mt-5 grid grid-cols-3 gap-x-6 gap-y-5 border-line-soft border-t pt-5">
        <Stat
          label="On disk"
          value={filled ? formatTrashBytes(trashTotalBytes(groups)) : '—'}
          sub={filled ? 'media + companions' : undefined}
        />
        <Stat
          label="Series"
          value={filled ? String(groups.length) : '—'}
          sub={filled ? 'with trashed files' : undefined}
        />
        <Stat
          label="Oldest"
          value={filled ? formatTrashAge(trashOldestAge(groups, now)) : '—'}
          sub={filled ? 'since trashing' : undefined}
        />
      </div>
    </header>
  );
}

function Stat({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <span className="font-mono text-[9.5px] text-muted uppercase tracking-[1px]">{label}</span>
      <span className="font-semibold text-[19px] text-ink leading-none tracking-[-0.4px] tabular-nums">
        {value}
      </span>
      {sub && <span className="text-[11.5px] text-muted leading-[1.35]">{sub}</span>}
    </div>
  );
}

/**
 * Filter / sort pill matching the library grid's controls: icon-only
 * square below `sm`, value at `sm`, label at `lg`.
 */
function TriggerPill({
  icon,
  label,
  value,
  ariaLabel,
  menuLabel,
  marked,
  children,
}: {
  icon: string;
  label: string;
  value: string;
  ariaLabel: string;
  menuLabel: string;
  marked?: boolean;
  children: ReactNode;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label={ariaLabel}
          className={cn(
            'relative inline-flex h-9 w-9 cursor-pointer items-center justify-center sm:w-auto sm:justify-start sm:gap-2 sm:px-3',
            'rounded-md border border-line-soft bg-surface text-sm text-ink shadow-card',
            'transition-[transform,box-shadow,background-color,color] duration-[160ms] ease-out',
            'hover:-translate-y-px hover:bg-overlay-soft hover:shadow-card-hover',
            'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-overlay',
          )}
        >
          <MaterialIcon name={icon} />
          <span className="hidden text-muted lg:inline">{label}</span>
          <span className="hidden sm:inline">{value}</span>
          <ChevronDown aria-hidden="true" className="hidden h-3.5 w-3.5 text-muted sm:inline" />
          {marked && (
            <span
              aria-hidden="true"
              className="absolute top-1 right-1 h-1.5 w-1.5 rounded-full bg-status-airing sm:hidden"
            />
          )}
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="min-w-[12rem]">
        <DropdownMenuLabel>{menuLabel}</DropdownMenuLabel>
        {children}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * One series. The body is a button opening the detail panel; the Empty
 * button is its sibling rather than a child, since a button inside a
 * button is invalid and would swallow the click.
 */
function SeriesRow({
  group,
  now,
  onOpen,
  onEmpty,
}: {
  group: TrashSeriesEntry;
  now: number;
  onOpen: () => void;
  onEmpty: () => void;
}) {
  const ages = group.entries.map((e) => trashEntryAge(e.trashedAt, now));
  const oldest = formatTrashAge(Math.max(...ages));
  const newest = formatTrashAge(Math.min(...ages));
  const bytes = formatTrashBytes(trashGroupBytes(group));
  return (
    <div className="flex items-center gap-3 border-line-soft border-b px-4 py-3 transition-colors last:border-b-0 hover:bg-overlay-soft">
      <button
        type="button"
        onClick={onOpen}
        aria-haspopup="dialog"
        className="flex min-w-0 flex-1 cursor-pointer items-center gap-3 text-left outline-none"
      >
        {/* `directory` is the whole series identity here — there is no
            second path line to render beneath it. */}
        <span className="min-w-0 flex-1 truncate font-medium text-[13.5px] text-ink tracking-[-0.1px]">
          {group.directory}
        </span>
        <span className="flex shrink-0 flex-col text-right sm:hidden">
          <span className="font-mono text-[11.5px] text-ink-2 tabular-nums">{bytes}</span>
          <span className="font-mono text-[10px] text-muted tabular-nums">{oldest}</span>
        </span>
        <span className="hidden w-[104px] shrink-0 text-right sm:block">
          <span className="font-mono text-[12px] text-ink-2 tabular-nums">{bytes}</span>
        </span>
        <span className="hidden w-[112px] shrink-0 flex-col text-right sm:flex">
          <span className="font-mono text-[11.5px] text-ink-2 tabular-nums">{newest} ago</span>
          <span className="font-mono text-[10.5px] text-muted tabular-nums">oldest {oldest}</span>
        </span>
        <MaterialIcon name="chevron_right" size={18} className="shrink-0 text-muted" />
      </button>
      <button
        type="button"
        onClick={onEmpty}
        title={`Empty trash for ${group.directory}`}
        aria-label={`Empty trash for ${group.directory}`}
        className={cn(
          'inline-flex h-[30px] shrink-0 cursor-pointer items-center gap-1.5 rounded-md border border-line-soft bg-paper px-2.5',
          'font-medium text-[12.5px] text-muted transition-colors duration-[120ms]',
          'hover:border-status-error hover:bg-status-error hover:text-status-error-fg',
        )}
      >
        <MaterialIcon name="delete_forever" size={15} />
        <span className="hidden lg:inline">Empty</span>
      </button>
    </div>
  );
}

/**
 * Per-series entry list. Bottom sheet below `sm`, centered dialog above
 * — the same shape as the episode-details sheet, minus its drag handle:
 * this panel is not draggable and a grab bar would promise otherwise.
 */
function SeriesDetail({
  group,
  now,
  onClose,
  onEmpty,
}: {
  group: TrashSeriesEntry | undefined;
  now: number;
  onClose: () => void;
  onEmpty: () => void;
}) {
  if (!group) {
    return null;
  }
  // Newest first — ULIDs are time-ordered, so the wire order is
  // chronological and reversing it is the whole sort.
  const entries = [...group.entries].reverse();
  return (
    <DialogPrimitive.Root open onOpenChange={(next) => !next && onClose()}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-[70] bg-overlay backdrop-blur-[2px]" />
        <div className="pointer-events-none fixed inset-0 z-[71] flex items-end justify-center sm:items-center sm:p-6">
          <DialogPrimitive.Content
            aria-describedby={undefined}
            className={cn(
              'pointer-events-auto relative flex max-h-[92dvh] w-full flex-col overflow-hidden',
              'rounded-t-[16px] bg-surface text-ink shadow-pop outline-none',
              'sm:max-h-[88dvh] sm:w-[580px] sm:max-w-[92vw] sm:rounded-[14px]',
            )}
          >
            <header className="flex shrink-0 items-start gap-2 border-line-soft border-b px-4 py-3">
              <MaterialIcon name="delete" size={16} className="mt-0.5 text-muted" />
              <DialogPrimitive.Title className="min-w-0 truncate font-semibold text-[15px] text-ink tracking-[-0.2px]">
                {group.directory}
              </DialogPrimitive.Title>
              <DialogPrimitive.Close asChild>
                <GhostIconButton size="lg" aria-label="Close" className="ml-auto">
                  <MaterialIcon name="close" size={18} />
                </GhostIconButton>
              </DialogPrimitive.Close>
            </header>
            {/* Entry counts belong here rather than in the page header:
                once the user has picked a series, "how many files" is
                what they are deciding about. */}
            <div className="flex shrink-0 items-center gap-3 border-line-soft border-b bg-surface-2 px-4 py-2.5 font-mono text-[11px] text-ink-2">
              <span>
                {group.entries.length} {group.entries.length === 1 ? 'entry' : 'entries'}
              </span>
              <span className="text-muted">·</span>
              <span>{formatTrashBytes(trashGroupBytes(group))}</span>
              <button
                type="button"
                onClick={onEmpty}
                className={cn(
                  'ml-auto inline-flex h-[28px] cursor-pointer items-center gap-1.5 rounded-md border border-status-error/40 px-2.5',
                  'font-medium text-[12px] text-status-error transition-colors',
                  'hover:bg-status-error hover:text-status-error-fg',
                )}
              >
                <MaterialIcon name="delete_forever" size={14} />
                Empty
              </button>
            </div>
            <div
              className="min-h-0 flex-1 overflow-y-auto bg-paper"
              style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
            >
              <div className="bg-surface">
                {entries.map((entry) => (
                  <EntryRow key={entry.id} entry={entry} now={now} />
                ))}
              </div>
              <p className="px-4 py-3 text-[11.5px] text-muted leading-[1.5]">
                Restoring is a CLI operation —{' '}
                <span className="font-mono text-[11px] text-ink-2">kura trash restore</span> puts
                files back at their recorded paths.
              </p>
            </div>
          </DialogPrimitive.Content>
        </div>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

function EntryRow({ entry, now }: { entry: TrashEntry; now: number }) {
  const [open, setOpen] = useState(false);
  const companions = entry.companions ?? [];
  const path = parseMediaPath(entry.mediaPath);
  return (
    <div className="border-line-soft border-b last:border-b-0">
      <div className="flex items-start gap-3 px-4 py-3">
        <span className="w-[52px] shrink-0 font-mono font-semibold text-[11px] text-ink">
          {shortMarker(entry.episode)}
        </span>
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="font-mono text-[12px] text-ink-2 leading-snug [overflow-wrap:anywhere]">
            {path.fileName || path.rel}
          </span>
          <div className="flex flex-wrap items-center gap-1.5">
            {entry.source && <SourceChip source={entry.source} size="compact" />}
            {entry.resolution && <ResolutionChip resolution={entry.resolution} size="compact" />}
            <span className="font-mono text-[10.5px] text-muted tabular-nums">
              {formatTrashBytes(trashEntryBytes(entry))}
            </span>
            <span className="font-mono text-[10.5px] text-muted tabular-nums">
              · {formatTrashStamp(entry.trashedAt)} ·{' '}
              {formatTrashAge(trashEntryAge(entry.trashedAt, now))} ago
            </span>
          </div>
          <span title={entry.id} className="truncate font-mono text-[10px] text-muted">
            {entry.id}
          </span>
          {companions.length > 0 && (
            <button
              type="button"
              onClick={() => setOpen((v) => !v)}
              aria-expanded={open}
              className="mt-0.5 inline-flex cursor-pointer items-center gap-1 self-start font-mono text-[10.5px] text-muted transition-colors hover:text-ink"
            >
              <MaterialIcon name={open ? 'expand_less' : 'expand_more'} size={13} />
              {companions.length} companion {companions.length === 1 ? 'file' : 'files'}
            </button>
          )}
          {open && (
            <ul className="mt-1 flex flex-col gap-1 border-line border-l pl-2.5">
              {companions.map((companion) => (
                <li
                  key={companion.path}
                  className="flex items-baseline justify-between gap-2 font-mono text-[10.5px] text-muted"
                >
                  <span className="truncate">
                    {parseMediaPath(companion.path).fileName || companion.path}
                  </span>
                  <span className="tabular-nums">{formatTrashBytes(companion.sizeBytes)}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}

/**
 * Confirm gate, mirroring the CLI's `--confirm`: what will be deleted,
 * the exact scope it applies to, and a destructive primary.
 */
function EmptyConfirm({
  target,
  ageLabel,
  busy,
  onCancel,
  onConfirm,
}: {
  target: EmptyTarget | null;
  ageLabel: string;
  busy: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  if (!target) {
    return null;
  }
  const entries = trashTotalEntries(target.groups);
  return (
    <DialogPrimitive.Root open onOpenChange={(next) => !next && onCancel()}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-[80] bg-overlay backdrop-blur-[2px]" />
        <div className="pointer-events-none fixed inset-0 z-[81] grid place-items-center px-5">
          <DialogPrimitive.Content
            aria-describedby={undefined}
            className="pointer-events-auto relative w-full max-w-[420px] rounded-[14px] bg-surface p-5 shadow-pop outline-none"
          >
            <div className="flex items-center gap-2.5">
              <span className="inline-flex h-8 w-8 items-center justify-center rounded-md bg-status-error/10 text-status-error">
                <MaterialIcon name="delete_forever" size={18} />
              </span>
              <DialogPrimitive.Title className="font-semibold text-[15px] text-ink tracking-[-0.2px]">
                {target.scope === 'all' ? 'Empty trash' : 'Empty series trash'}
              </DialogPrimitive.Title>
            </div>
            <p className="mt-3 text-[13px] text-ink-2 leading-[1.55]">
              Permanently deletes {entries} {entries === 1 ? 'entry' : 'entries'} (
              {formatTrashBytes(trashTotalBytes(target.groups))}) from disk. Files can no longer be
              restored afterwards.
            </p>
            <dl className="mt-3 flex flex-col gap-1 rounded-[8px] bg-paper px-3 py-2.5 font-mono text-[11px]">
              <div className="flex justify-between gap-3">
                <dt className="text-muted">scope</dt>
                <dd className="truncate text-ink-2">
                  {target.scope === 'all'
                    ? `all series · ${target.groups.length}`
                    : (target.groups[0]?.directory ?? '—')}
                </dd>
              </div>
              <div className="flex justify-between gap-3">
                <dt className="text-muted">older than</dt>
                <dd className="text-ink-2">{ageLabel}</dd>
              </div>
            </dl>
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
                onClick={onConfirm}
                disabled={busy}
                className={cn(
                  'inline-flex h-9 cursor-pointer items-center gap-2 rounded-md bg-status-error px-3.5',
                  'font-medium text-sm text-status-error-fg shadow-card',
                  'transition-[transform,box-shadow] duration-[160ms] hover:-translate-y-px hover:shadow-card-hover',
                  busy && 'cursor-not-allowed opacity-60',
                )}
              >
                <MaterialIcon name="delete_forever" size={16} />
                Empty permanently
              </button>
            </div>
          </DialogPrimitive.Content>
        </div>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

const RESULT_STYLE: Record<TrashOutcome['kind'], { icon: string; tone: string }> = {
  success: { icon: 'check_circle', tone: 'text-status-complete' },
  partial: { icon: 'warning', tone: 'text-status-incomplete' },
  none: { icon: 'info', tone: 'text-muted' },
};

/**
 * Outcome banner. Success and "nothing matched" self-expire after six
 * seconds with a drain bar; partial never does — it names series and
 * reasons the user has to actually read.
 */
function ResultBanner({
  result,
  scopeNote,
  topGap,
  onDismiss,
}: {
  result: TrashOutcome;
  scopeNote: string;
  topGap: boolean;
  onDismiss: () => void;
}) {
  const style = RESULT_STYLE[result.kind];
  const expires = result.kind !== 'partial';

  // `result` is a dependency so a banner REPLACED by a second empty
  // action restarts the countdown; `onDismiss` must be
  // identity-stable in the caller or every parent re-render resets it.
  // biome-ignore lint/correctness/useExhaustiveDependencies: `result` is deliberately extra — the timer must restart when the banner content is swapped, not only when it mounts.
  useEffect(() => {
    if (!expires) {
      return;
    }
    const timer = setTimeout(onDismiss, BANNER_TTL_MS);
    return () => clearTimeout(timer);
  }, [expires, onDismiss, result]);

  const title =
    result.kind === 'none'
      ? 'Nothing matched — trash left untouched.'
      : result.kind === 'partial'
        ? `Emptied ${result.entries} ${result.entries === 1 ? 'entry' : 'entries'} · reclaimed ${formatTrashBytes(result.bytes)} · ${result.failures.length} series failed`
        : `Emptied ${result.entries} ${result.entries === 1 ? 'entry' : 'entries'} · reclaimed ${formatTrashBytes(result.bytes)}`;

  return (
    <div className={cn('mb-4 flex items-start gap-2.5', topGap && 'mt-5')}>
      <div className="relative flex w-full items-start gap-2.5 overflow-hidden rounded-[10px] border border-line-soft bg-surface px-4 py-3 shadow-card">
        <MaterialIcon name={style.icon} size={16} className={cn('mt-px shrink-0', style.tone)} />
        <div className="flex min-w-0 flex-1 flex-col gap-1.5">
          <p className="font-medium text-[13px] text-ink">{title}</p>
          {result.failures.map((failure) => (
            <p
              key={failure.directory}
              className="font-mono text-[11px] text-muted leading-[1.5] [overflow-wrap:anywhere]"
            >
              <span className="text-status-error">failed</span> {failure.directory} —{' '}
              {failure.error}
            </p>
          ))}
          {result.kind !== 'partial' && <p className="text-[12px] text-muted">{scopeNote}</p>}
        </div>
        <button
          type="button"
          onClick={onDismiss}
          aria-label="Dismiss"
          className="shrink-0 cursor-pointer text-muted transition-colors hover:text-ink"
        >
          <MaterialIcon name="close" size={15} />
        </button>
        {expires && (
          <span aria-hidden="true" className="absolute inset-x-0 bottom-0 h-[2px] bg-line-soft">
            <span className="kura-toast-bar block h-full origin-left bg-status-complete/70" />
          </span>
        )}
      </div>
    </div>
  );
}

function TrashBlank({
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
