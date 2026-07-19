import * as DialogPrimitive from '@radix-ui/react-dialog';
import { type ReactNode, type RefObject, useId, useState } from 'react';

import type { CompanionShow, EpisodeShow, EpisodeStatus, MediaShow } from '@/api/types';
import { ResolutionChip, SourceChip } from '@/components/series/QualityChip';
import { CopyButton } from '@/components/ui/copy-button';
import { GhostIconButton } from '@/components/ui/ghost-icon-btn';
import { MaterialIcon } from '@/components/ui/material-icon';
import { cn } from '@/lib/cn';
import { EPISODE_STATUS_BADGE } from '@/lib/episodeStatus';
import { formatDateTime, formatSize, shortMarker } from '@/lib/format';
import { diffMedia } from '@/lib/mediaDiff';
import { parseMediaPath } from '@/lib/mediaPath';
import { formatRelativeAgo } from '@/lib/relativeTime';

interface EpisodeDetailsSheetProps {
  episode: EpisodeShow;
  seriesDir: string | undefined;
  lastScanned: string | undefined;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  restoreFocusRef: RefObject<HTMLButtonElement | null>;
}

export function EpisodeDetailsSheet({
  episode,
  seriesDir,
  lastScanned,
  open,
  onOpenChange,
  restoreFocusRef,
}: EpisodeDetailsSheetProps) {
  const status = episode.status;
  const scannedAgo = formatRelativeAgo(lastScanned) || 'unknown';

  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-[70] bg-overlay backdrop-blur-[2px] data-[state=open]:animate-in data-[state=open]:fade-in-0" />
        <div className="pointer-events-none fixed inset-0 z-[71] flex items-end justify-center sm:items-center sm:p-6">
          <DialogPrimitive.Content
            aria-describedby={undefined}
            onCloseAutoFocus={(event) => {
              event.preventDefault();
              restoreFocusRef.current?.focus();
            }}
            className={cn(
              'pointer-events-auto relative flex max-h-[92dvh] w-full flex-col overflow-hidden',
              'rounded-t-[16px] bg-surface text-ink shadow-pop outline-none',
              'sm:max-h-[88dvh] sm:w-[480px] sm:max-w-[92vw] sm:rounded-[14px]',
            )}
          >
            <div className="flex shrink-0 justify-center pt-2 pb-0.5 sm:hidden">
              <span className="h-1 w-9 rounded-full bg-line" />
            </div>
            <header className="flex shrink-0 items-center gap-2 border-line-soft border-b px-4 py-3">
              <MaterialIcon name="movie" size={16} className="text-muted" />
              <DialogPrimitive.Title className="text-[15px] font-semibold text-ink">
                Episode details
              </DialogPrimitive.Title>
              <DialogPrimitive.Close asChild>
                <GhostIconButton size="lg" aria-label="Close episode details" className="ml-auto">
                  <MaterialIcon name="close" size={18} />
                </GhostIconButton>
              </DialogPrimitive.Close>
            </header>
            <div className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden bg-paper">
              <div className="pt-3">
                <EpisodeSummary episode={episode} status={status} />
              </div>
              <MediaTabs episode={episode} seriesDir={seriesDir} status={status} />
              <div className="flex items-center gap-1.5 border-line-soft border-t bg-surface-2 px-4 py-2.5">
                <MaterialIcon name="cloud_sync" size={13} className="shrink-0 text-muted" />
                <span className="min-w-0 font-mono text-[10px] text-muted break-words">
                  Library data (whole series) last scanned {scannedAgo}
                </span>
              </div>
            </div>
          </DialogPrimitive.Content>
        </div>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

function EpisodeSummary({ episode, status }: { episode: EpisodeShow; status: EpisodeStatus }) {
  const title = episode.preferredTitle || episode.canonicalTitle || episode.episode;
  return (
    <div className="px-4 pb-1">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-mono text-[15px] font-bold tracking-[0.5px] text-ink">
          {shortMarker(episode.episode)}
        </span>
        <StatusBadge status={status} />
      </div>
      <h3 className="mt-2 font-serif text-[19px] leading-tight font-semibold text-ink [text-wrap:pretty]">
        {title}
      </h3>
      {episode.canonicalTitle && episode.canonicalTitle !== title && (
        <div className="mt-0.5 text-[12px] text-muted [text-wrap:pretty]">
          {episode.canonicalTitle}
        </div>
      )}
      <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-[11px] text-muted">
        <span className="flex items-center gap-1">
          <MaterialIcon name="event" size={12} />
          {episode.aired || 'Air date unknown'}
        </span>
      </div>
    </div>
  );
}

function StatusBadge({ status }: { status: EpisodeStatus }) {
  const badge = EPISODE_STATUS_BADGE[status];
  return (
    <span
      className={cn(
        'inline-flex h-[24px] shrink-0 items-center gap-1.5 rounded-md px-2 font-mono text-[10px] font-bold tracking-[0.6px] uppercase',
        badge.className,
      )}
    >
      <MaterialIcon name={badge.icon} size={13} />
      {badge.label}
    </span>
  );
}

function MediaTabs({
  episode,
  seriesDir,
  status,
}: {
  episode: EpisodeShow;
  seriesDir: string | undefined;
  status: EpisodeStatus;
}) {
  const hasActive = !!episode.active;
  const hasStaged = !!episode.staged;
  const [tab, setTab] = useState<'active' | 'staged'>(hasActive ? 'active' : 'staged');
  const id = useId();
  const activeTabId = `${id}-tab-active`;
  const activePanelId = `${id}-panel-active`;
  const stagedTabId = `${id}-tab-staged`;
  const stagedPanelId = `${id}-panel-staged`;

  if (!hasActive && !hasStaged) {
    return <EmptyMedia status={status} />;
  }

  const stagedContext = episode.staged
    ? stagedMediaContext(episode.staged, episode.active)
    : undefined;

  return (
    <div className="px-4 pt-2 pb-4">
      <div
        role="tablist"
        aria-label="Media files"
        className="mb-3 inline-flex w-full rounded-lg bg-line p-0.5"
      >
        <TabButton
          id={activeTabId}
          panelId={activePanelId}
          icon="folder_open"
          label="Current file"
          active={tab === 'active'}
          disabled={!hasActive}
          hint={!hasActive ? 'none' : undefined}
          onClick={() => setTab('active')}
        />
        <TabButton
          id={stagedTabId}
          panelId={stagedPanelId}
          icon="move_to_inbox"
          label="Staged file"
          active={tab === 'staged'}
          disabled={!hasStaged}
          hint={!hasStaged ? 'none' : undefined}
          onClick={() => setTab('staged')}
        />
      </div>
      {tab === 'active' && episode.active && (
        <div role="tabpanel" id={activePanelId} aria-labelledby={activeTabId}>
          <MediaCard media={episode.active} seriesDir={seriesDir} />
        </div>
      )}
      {tab === 'staged' && episode.staged && (
        <div
          role="tabpanel"
          id={stagedPanelId}
          aria-labelledby={stagedTabId}
          className="flex flex-col gap-3"
        >
          {episode.active && <ChangeSummary from={episode.active} to={episode.staged} />}
          <MediaCard media={episode.staged} seriesDir={seriesDir} context={stagedContext} />
        </div>
      )}
    </div>
  );
}

function TabButton({
  active,
  disabled,
  onClick,
  icon,
  label,
  hint,
  id,
  panelId,
}: {
  active: boolean;
  disabled: boolean;
  onClick: () => void;
  icon: string;
  label: string;
  hint?: string;
  id: string;
  panelId: string;
}) {
  return (
    <button
      type="button"
      role="tab"
      id={id}
      aria-controls={panelId}
      aria-selected={active}
      disabled={disabled}
      onClick={onClick}
      className={cn(
        // radius-lg (14px) track minus 2px padding → 12px chip keeps the arcs concentric
        'inline-flex h-[34px] flex-1 cursor-pointer items-center justify-center gap-1.5 rounded-[12px] px-2 text-[12px] font-medium transition-colors',
        active ? 'bg-surface text-ink shadow-card' : 'text-muted hover:text-ink',
        disabled && 'cursor-not-allowed opacity-45 hover:text-muted',
      )}
    >
      <MaterialIcon name={icon} size={15} className={active ? 'text-ink' : 'text-muted'} />
      <span>{label}</span>
      {hint && (
        <span className="font-mono text-[9px] tracking-[0.4px] text-muted uppercase">· {hint}</span>
      )}
    </button>
  );
}

function EmptyMedia({ status }: { status: EpisodeStatus }) {
  const pending = status === 'pending';
  return (
    <div className="px-4 pt-2 pb-4">
      <div className="flex flex-col items-center gap-2 rounded-[12px] border border-dashed border-line bg-surface-2 px-4 py-8 text-center">
        <MaterialIcon
          name={pending ? 'schedule' : 'inventory_2'}
          size={26}
          className="text-muted"
        />
        <div className="text-[13px] font-medium text-ink">No active or staged media</div>
        <div className="max-w-[36ch] text-[12px] text-muted [text-wrap:pretty]">
          {pending
            ? 'This episode has not aired yet. Nothing is on disk and nothing is staged.'
            : 'Kura has no file on disk for this episode and nothing is queued to import.'}
        </div>
      </div>
    </div>
  );
}

interface MediaContext {
  icon: string;
  text: string;
}

function stagedMediaContext(staged: MediaShow, active: MediaShow | undefined): MediaContext {
  if (active && staged.file === active.file) {
    return { icon: 'edit_note', text: 'In-place metadata update — same library file' };
  }
  if (active) {
    return { icon: 'swap_horiz', text: 'Will replace the current file' };
  }
  return { icon: 'hourglass_top', text: 'Awaiting reconcile — no active file yet' };
}

function MediaCard({
  media,
  seriesDir,
  context,
}: {
  media: MediaShow;
  seriesDir: string | undefined;
  context?: MediaContext;
}) {
  const path = parseMediaPath(media.file, seriesDir);
  const companions = media.companions ?? [];
  const companionSize = companions.reduce((total, companion) => total + companion.size, 0);
  const footprint = media.size + companionSize;

  return (
    <section className="overflow-hidden rounded-[12px] bg-surface shadow-card">
      {context && (
        <div className="flex items-center gap-1.5 border-line-soft border-b bg-surface-2 px-4 py-2 text-[11px] text-ink-2">
          <MaterialIcon name={context.icon} size={13} className="shrink-0 text-muted" />
          <span className="min-w-0 break-words">{context.text}</span>
        </div>
      )}
      <div className="flex flex-wrap items-center gap-1.5 px-4 pt-3.5 pb-3">
        <SourceChip source={media.source} size="compact" />
        {media.resolution && <ResolutionChip resolution={media.resolution} size="compact" />}
      </div>
      <div className="grid grid-cols-2 gap-x-4 gap-y-3 px-4 pb-3.5">
        <Fact label="Dimensions">{media.dimensions || '—'}</Fact>
        <Fact label="Video codec">{media.codec || '—'}</Fact>
        <Fact label="File size">{formatSize(media.size)}</Fact>
        <Fact label="Modified">{formatDateTime(media.mtime) || '—'}</Fact>
        <Fact label="Container">{path.ext ? `.${path.ext}` : '—'}</Fact>
      </div>
      <div className="flex items-start gap-2 border-line-soft border-t px-4 py-3">
        <div className="min-w-0 flex-1">
          <div className="font-mono text-[9px] tracking-[0.8px] text-muted uppercase">Path</div>
          <div className="mt-1 font-mono text-[12px] leading-snug text-ink [overflow-wrap:anywhere] [word-break:break-all]">
            {path.rel || '—'}
          </div>
        </div>
        <div className="-my-1 flex shrink-0 gap-0.5">
          <CopyButton
            variant="icon"
            text={path.portable}
            what="portable path"
            label={`Copy path for ${path.fileName || 'media file'}`}
          />
          <CopyButton
            variant="icon"
            icon="tag"
            text={media.file}
            what="Kura selector"
            label={`Copy Kura selector for ${path.fileName || 'media file'}`}
          />
        </div>
      </div>
      {companions.length > 0 && (
        <div className="mx-4 mb-3.5 flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg bg-surface-2 px-3 py-2 font-mono text-[10px] text-ink-2">
          <span className="flex items-center gap-1">
            <MaterialIcon name="functions" size={12} className="text-muted" />
            Footprint
          </span>
          <span>{formatSize(media.size)} file</span>
          <span className="text-muted">+</span>
          <span>
            {companions.length} companion{companions.length > 1 ? 's' : ''}{' '}
            {formatSize(companionSize)}
          </span>
          <span className="ml-auto font-semibold text-ink">{formatSize(footprint)} total</span>
        </div>
      )}
      {companions.length > 0 && (
        <div>
          <CardSub icon="attachment" count={companions.length}>
            Companion files
          </CardSub>
          <div className="pb-1">
            {companions.map((companion) => (
              <CompanionRow key={companion.path} companion={companion} seriesDir={seriesDir} />
            ))}
          </div>
        </div>
      )}
      <Attributes attrs={media.attrs} />
    </section>
  );
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <div className="font-mono text-[9px] tracking-[0.8px] text-muted uppercase">{label}</div>
      <div className="mt-0.5 font-mono text-[12px] text-ink break-words [overflow-wrap:anywhere]">
        {children}
      </div>
    </div>
  );
}

function CardSub({ icon, children, count }: { icon: string; children: ReactNode; count?: number }) {
  return (
    <div className="flex items-center gap-1.5 border-line-soft border-t px-4 pt-3 pb-1">
      <MaterialIcon name={icon} size={13} className="text-muted" />
      <span className="font-mono text-[9px] font-bold tracking-[1.2px] text-muted uppercase">
        {children}
      </span>
      {count != null && <span className="font-mono text-[9px] text-muted">· {count}</span>}
    </div>
  );
}

function CompanionRow({
  companion,
  seriesDir,
}: {
  companion: CompanionShow;
  seriesDir: string | undefined;
}) {
  const path = parseMediaPath(companion.path, seriesDir);
  const meta = [
    { name: 'role', value: companion.role },
    { name: 'language', value: companion.language },
    { name: 'label', value: companion.label },
  ].filter((item): item is { name: string; value: string } => !!item.value);
  return (
    <div className="flex items-start gap-2 border-line-soft border-t px-4 py-2 first:border-t-0">
      <MaterialIcon name="attachment" size={14} className="mt-0.5 shrink-0 text-muted" />
      <div className="min-w-0 flex-1">
        <div className="font-mono text-[12px] text-ink [overflow-wrap:anywhere] [word-break:break-all]">
          {path.fileName || path.rel || '—'}
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-1">
          {meta.map((item) => (
            <span
              key={item.name}
              className="inline-flex h-[18px] items-center rounded border border-line-soft bg-surface-2 px-1.5 font-mono text-[9px] text-ink-2 lowercase"
            >
              {item.value}
            </span>
          ))}
          <span className="font-mono text-[10px] text-muted">{formatSize(companion.size)}</span>
          {companion.mtime && (
            <span className="font-mono text-[10px] text-muted">
              · {formatDateTime(companion.mtime)}
            </span>
          )}
        </div>
      </div>
      <div className="-my-1 flex shrink-0 gap-0.5">
        <CopyButton
          variant="icon"
          text={path.portable}
          what={`portable path for ${path.fileName || 'companion file'}`}
          label={`Copy path for ${path.fileName || 'companion file'}`}
        />
        <CopyButton
          variant="icon"
          icon="tag"
          text={companion.path}
          what={`Kura selector for ${path.fileName || 'companion file'}`}
          label={`Copy Kura selector for ${path.fileName || 'companion file'}`}
        />
      </div>
    </div>
  );
}

function Attributes({ attrs }: { attrs: MediaShow['attrs'] }) {
  const values = attrs ?? {};
  const entries = Object.entries(values).sort(([left], [right]) => left.localeCompare(right));
  if (entries.length === 0) {
    return null;
  }
  const sorted = Object.fromEntries(entries);
  const json = JSON.stringify(sorted, null, 2);

  return (
    <div>
      <div className="flex items-center gap-1.5 border-line-soft border-t px-4 pt-3 pb-1">
        <MaterialIcon name="data_object" size={13} className="text-muted" />
        <span className="font-mono text-[9px] font-bold tracking-[1.2px] text-muted uppercase">
          Adoption attributes
        </span>
        <span className="font-mono text-[9px] text-muted">· {entries.length}</span>
        <CopyButton
          className="ml-auto"
          text={json}
          what="attributes as JSON"
          label="Copy JSON"
          icon="data_object"
        />
      </div>
      <div className="px-4 pt-1 pb-3">
        <dl className="divide-y divide-line-soft rounded-lg bg-surface-2">
          {entries.map(([key, value]) => (
            <div key={key} className="flex items-start gap-2 px-2.5 py-2">
              <dt className="w-[34%] shrink-0 font-mono text-[10px] text-muted break-words [overflow-wrap:anywhere]">
                {key}
              </dt>
              <dd className="min-w-0 flex-1 font-mono text-[11px] text-ink [overflow-wrap:anywhere] [word-break:break-word]">
                {value}
              </dd>
              <CopyButton
                variant="icon"
                className="-my-0.5 h-7 w-7"
                text={value}
                what={`value of ${key}`}
                label={`Copy ${key}`}
              />
            </div>
          ))}
        </dl>
      </div>
    </div>
  );
}

function ChangeSummary({ from, to }: { from: MediaShow; to: MediaShow }) {
  const rows = diffMedia(from, to);
  if (rows.length === 0) {
    return null;
  }
  return (
    <section className="overflow-hidden rounded-[12px] bg-surface shadow-card">
      <div className="flex items-center gap-1.5 bg-surface-2 px-4 py-2.5">
        <MaterialIcon name="difference" size={14} className="text-muted" />
        <span className="font-mono text-[10px] font-bold tracking-[1.2px] text-ink uppercase">
          What will change
        </span>
      </div>
      <div className="divide-y divide-line-soft">
        {rows.map((row) => (
          <div key={row.label} className="flex items-center gap-2 px-4 py-2 text-[12px]">
            <span className="w-[74px] shrink-0 font-mono text-[10px] tracking-[0.4px] text-muted uppercase">
              {row.label}
            </span>
            {row.note ? (
              <span className="min-w-0 text-ink-2 break-words">{row.note}</span>
            ) : (
              <span className="flex min-w-0 flex-wrap items-center gap-1.5 font-mono text-[11px] [overflow-wrap:anywhere]">
                <span className="text-muted line-through">{row.from}</span>
                <MaterialIcon name="arrow_forward" size={12} className="text-muted" />
                <span className="font-semibold text-ink">{row.to}</span>
              </span>
            )}
          </div>
        ))}
      </div>
    </section>
  );
}
