import { type ReactNode, useEffect, useState } from 'react';

import { useLibraryJob } from '@/api/libraryJob';
import { MaterialIcon } from '@/components/ui/material-icon';
import { cn } from '@/lib/cn';
import type { LibraryJobKind } from '@/lib/libraryJobState';
import type { DensityPreference } from '@/lib/useAutoDensity';
import { useDensityPreference } from '@/state/density';
import { useSearchPrefs } from '@/state/searchPrefs';
import { type Theme, useTheme } from '@/state/theme';

const THEME_OPTIONS: ReadonlyArray<SegmentOption<Theme>> = [
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
  { value: 'system', label: 'System' },
];

const DENSITY_OPTIONS: ReadonlyArray<SegmentOption<DensityPreference>> = [
  { value: 'comfortable', label: 'Comfortable' },
  { value: 'compact', label: 'Compact' },
];

const JOB_LABEL: Record<LibraryJobKind, string> = {
  scan: 'Re-scanning library',
  reindex: 'Re-indexing library',
};

/** How long the finished state stays up before the buttons return. */
const DONE_HOLD_MS = 1800;

/**
 * Settings — the app's one preferences surface, replacing the top-bar
 * gear menu. Two cards today: Appearance (theme + grid density, both
 * persisted client-side) and Library maintenance (the scan / reindex
 * jobs the gear menu used to launch).
 *
 * Row layout rule: narrow controls (the run buttons) sit right of
 * their label at every width; wide controls (the segmented pickers)
 * only sit right from `sm` up and drop to their own full-width line
 * below that.
 */
export function SettingsPage() {
  const theme = useTheme((s) => s.theme);
  const setTheme = useTheme((s) => s.setTheme);
  const density = useDensityPreference((s) => s.preference);
  const setDensity = useDensityPreference((s) => s.setPreference);
  const animeOnly = useSearchPrefs((s) => s.animeOnly);
  const setAnimeOnly = useSearchPrefs((s) => s.setAnimeOnly);

  return (
    <div className="mx-auto w-full max-w-[720px] px-6 py-8">
      {/* Below md the PageMobileBar already titles the page;
          rendering the h1 too would say "Settings" twice in a row. */}
      <h1 className="hidden font-semibold text-[22px] text-ink tracking-[-0.3px] md:block">
        Settings
      </h1>
      <div className="mt-5 flex flex-col gap-4 max-md:mt-0">
        <SettingsCard label="Appearance">
          <SettingRow label="Theme">
            <Segmented
              ariaLabel="Theme"
              value={theme}
              options={THEME_OPTIONS}
              onChange={setTheme}
            />
          </SettingRow>
          <SettingRow label="Grid density" sub="Poster size in the library grid">
            <Segmented
              ariaLabel="Grid density"
              value={density}
              options={DENSITY_OPTIONS}
              onChange={setDensity}
            />
          </SettingRow>
        </SettingsCard>
        <SettingsCard label="Library maintenance">
          <LibraryMaintenance />
        </SettingsCard>
        <SettingsCard label="Search">
          <SettingRow inline label="Anime only" sub="Only include anime in TVDB search results">
            <Switch
              ariaLabel="Only include anime in TVDB search results"
              checked={animeOnly}
              onChange={setAnimeOnly}
            />
          </SettingRow>
        </SettingsCard>
      </div>
    </div>
  );
}

function SettingsCard({ label, children }: { label: string; children: ReactNode }) {
  return (
    <section className="rounded-[14px] border border-line-soft bg-surface p-5 shadow-card">
      <h2 className="mb-4 font-mono text-[10px] text-muted uppercase tracking-[1.2px]">{label}</h2>
      <div className="flex flex-col gap-4">{children}</div>
    </section>
  );
}

interface SettingRowProps {
  label: string;
  sub?: string;
  /**
   * Narrow control: stays beside the label at every width. Wide
   * controls (the default) drop below the label under `sm`.
   */
  inline?: boolean;
  children: ReactNode;
}

function SettingRow({ label, sub, inline, children }: SettingRowProps) {
  return (
    <div
      className={cn(
        'flex',
        inline
          ? 'flex-row items-center justify-between gap-6'
          : 'flex-col gap-2.5 sm:flex-row sm:items-center sm:justify-between sm:gap-6',
      )}
    >
      <div className="flex min-w-0 flex-col">
        <span className="font-medium text-ink text-sm">{label}</span>
        {sub && <span className="mt-0.5 text-[12px] text-muted">{sub}</span>}
      </div>
      {children}
    </div>
  );
}

interface SegmentOption<T extends string> {
  value: T;
  label: string;
}

interface SegmentedProps<T extends string> {
  value: T;
  options: ReadonlyArray<SegmentOption<T>>;
  onChange: (value: T) => void;
  ariaLabel: string;
}

/**
 * Radio group rendered as a segmented control. Full-width with equal
 * segments below `sm` (where it owns its own row) and content-sized
 * from `sm` up (where it sits beside the label).
 */
function Segmented<T extends string>({ value, options, onChange, ariaLabel }: SegmentedProps<T>) {
  return (
    <div
      role="radiogroup"
      aria-label={ariaLabel}
      className="inline-flex h-9 w-full shrink-0 items-stretch gap-0.5 rounded-[8px] border border-line-soft bg-paper p-0.5 sm:w-auto"
    >
      {options.map((o) => (
        // biome-ignore lint/a11y/useSemanticElements: an <input type="radio"> can't carry the segment fill without a parallel <label> per option; button + radiogroup is the shape the design reference specifies.
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          onClick={() => onChange(o.value)}
          className={cn(
            'inline-flex flex-1 cursor-pointer items-center justify-center rounded-[5px] px-3',
            'font-medium text-[13px] transition-colors duration-[120ms] sm:flex-none',
            value === o.value ? 'bg-ink text-paper' : 'text-muted hover:text-ink',
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

/** Toggle switch — the narrow control in a boolean setting row. */
function Switch({
  checked,
  onChange,
  ariaLabel,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  ariaLabel: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={ariaLabel}
      onClick={() => onChange(!checked)}
      className={cn(
        'relative h-[22px] w-[38px] shrink-0 cursor-pointer rounded-full transition-colors duration-[160ms]',
        checked ? 'bg-status-complete' : 'bg-line',
      )}
    >
      <span
        className={cn(
          'absolute top-[2px] h-[18px] w-[18px] rounded-full bg-surface shadow-card transition-[left] duration-[160ms]',
          checked ? 'left-[18px]' : 'left-[2px]',
        )}
      />
    </button>
  );
}

/**
 * Library maintenance card body. Idle shows one row per job; while a
 * job runs (and for a short hold after it finishes) the rows are
 * replaced wholesale by its progress — only one library-wide job can
 * run at a time, so there is nothing else to offer meanwhile.
 */
function LibraryMaintenance() {
  const job = useLibraryJob();
  // `runKind` outlives `job.phase === 'running'` by DONE_HOLD_MS so
  // the finished state has a moment on screen instead of snapping
  // back to the buttons the instant the last file lands.
  const [runKind, setRunKind] = useState<LibraryJobKind | null>(job.kind ?? null);
  const [done, setDone] = useState(false);

  useEffect(() => {
    if (job.phase === 'running') {
      setRunKind(job.kind ?? 'scan');
      setDone(false);
      return;
    }
    if (job.lastError) {
      // Failed jobs get no green "finished" beat — drop straight back
      // to the buttons so the error line under them tells the story.
      setRunKind(null);
      setDone(false);
      return;
    }
    setDone(true);
    const timer = setTimeout(() => {
      setRunKind(null);
      setDone(false);
    }, DONE_HOLD_MS);
    return () => clearTimeout(timer);
  }, [job.phase, job.kind, job.lastError]);

  return (
    <>
      {runKind ? (
        <JobProgress
          kind={runKind}
          done={done}
          current={job.progress?.current ?? 0}
          total={job.progress?.total ?? 0}
          message={job.progress?.message ?? ''}
        />
      ) : (
        <>
          <SettingRow inline label="Rescan library" sub="Update metadata and look for file changes">
            <RunButton onClick={job.startScan} label="Run library rescan" />
          </SettingRow>
          <SettingRow
            inline
            label="Rebuild index"
            sub="Rebuild cached library index from series data"
          >
            <RunButton onClick={job.startReindex} label="Run index rebuild" />
          </SettingRow>
        </>
      )}
      {job.lastError && <p className="text-[12px] text-status-error">{job.lastError.message}</p>}
    </>
  );
}

interface JobProgressProps {
  kind: LibraryJobKind;
  done: boolean;
  current: number;
  total: number;
  message: string;
}

/**
 * Meta row ports the gear menu's information layout: the series
 * currently being processed on the left, the done/total counter on
 * the right. Scan progress carries bare series names; reindex
 * messages arrive as "Indexing <ref>" — the verb is dropped since the
 * title line above already says what is running. Names truncate with
 * a trailing ellipsis: the leading words are the recognizable part of
 * a series title. The done beat drops the row — the hook discards
 * progress at terminal, and made-up numbers are worse than none.
 */
function JobProgress({ kind, done, current, total, message }: JobProgressProps) {
  const series = message.replace(/^Indexing\s+/, '');
  // The server reports totals only once it has walked the tree; until
  // then there is no ratio to draw, so the bar sweeps instead.
  const indeterminate = !done && total <= 0;
  const percent = total > 0 ? Math.round(Math.min(1, current / total) * 100) : 0;

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2 font-medium text-ink text-sm">
        {done ? (
          <MaterialIcon name="check_circle" size={16} className="text-status-complete" />
        ) : (
          <MaterialIcon name="progress_activity" size={15} className="animate-spin text-muted" />
        )}
        {done ? `${JOB_LABEL[kind]} — finished` : `${JOB_LABEL[kind]}…`}
      </div>
      <div className="h-1.5 overflow-hidden rounded-full bg-line-soft">
        <div
          className={cn(
            'h-full rounded-full transition-[width] duration-300',
            done ? 'bg-status-complete' : 'bg-status-airing',
            indeterminate && 'w-1/3 animate-pulse',
          )}
          style={indeterminate ? undefined : { width: `${done ? 100 : percent}%` }}
        />
      </div>
      {!done && (
        <div className="flex items-baseline justify-between gap-2.5 font-mono text-[11px] text-muted">
          <span className="min-w-0 flex-1 truncate">{series || '…'}</span>
          <span className="shrink-0 font-semibold text-ink tabular-nums">
            {indeterminate
              ? current.toLocaleString()
              : `${current.toLocaleString()} / ${total.toLocaleString()}`}
          </span>
        </div>
      )}
    </div>
  );
}

/** 34 px square run trigger — the narrow control in a maintenance row. */
function RunButton({ onClick, label }: { onClick: () => void; label: string }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      title="Run"
      className={cn(
        'inline-flex h-[34px] w-[34px] shrink-0 cursor-pointer items-center justify-center',
        'rounded-[6px] border border-line-soft bg-paper text-ink',
        'transition-[background-color,transform] duration-[160ms] ease-out',
        'hover:-translate-y-px hover:bg-overlay-soft',
      )}
    >
      <MaterialIcon name="play_arrow" size={18} />
    </button>
  );
}
