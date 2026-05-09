import { useEffect, useState } from 'react';

import { useLibraryJob } from '@/api/libraryJob';
import { Logo } from '@/components/Logo';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { MaterialIcon } from '@/components/ui/material-icon';
import { type Theme, useTheme } from '@/state/theme';

import styles from './GearMenu.module.css';

const THEME_OPTIONS: ReadonlyArray<{ value: Theme; label: string; icon: string; sub?: string }> = [
  { value: 'light', label: 'Light', icon: 'light_mode' },
  { value: 'dark', label: 'Dark', icon: 'dark_mode' },
  { value: 'system', label: 'System', icon: 'brightness_auto', sub: 'match OS' },
];

const APPEARANCE_FADE_MS = 180;

/**
 * Gear button + popout. Opens a sectioned admin menu:
 *   - Header: kura mark + name.
 *   - Library actions (idle): Scan library, Rebuild index.
 *   - Running section (job in flight): title + progress bar + done /
 *     total counter + currently-processing path.
 *   - Appearance: light / dark / system.
 *
 * The trigger glyph swaps from gear → ring spinner while a library
 * job is running. The popout body swaps the Library section for the
 * Running section in the same transition. Anti-flicker handoff: when
 * the job ends, hold the running view for ~180 ms so the ring fades
 * out before the body re-flips to library actions.
 *
 * Backed by `useLibraryJob`, which polls `/api/v1/jobs/{id}` for the
 * single in-flight library job and persists the running record to
 * localStorage so reload/cross-tab restores it.
 */
interface GearMenuProps {
  /** Storybook seam: open the popout on first render so the menu body
   * is visible without an interaction. Production callers omit this. */
  defaultOpen?: boolean;
}

export function GearMenu({ defaultOpen }: GearMenuProps = {}) {
  const theme = useTheme((s) => s.theme);
  const setTheme = useTheme((s) => s.setTheme);
  const job = useLibraryJob();

  // Anti-flicker handoff: when phase transitions running → idle, hold
  // the running view for APPEARANCE_FADE_MS so the ring fade and
  // section swap don't collide in the same frame.
  const [runningView, setRunningView] = useState<boolean>(job.phase === 'running');
  useEffect(() => {
    if (job.phase === 'running') {
      setRunningView(true);
      return;
    }
    const t = setTimeout(() => setRunningView(false), APPEARANCE_FADE_MS);
    return () => clearTimeout(t);
  }, [job.phase]);

  const showRing = job.phase === 'running' || runningView;
  const total = job.progress?.total ?? 0;
  const current = job.progress?.current ?? 0;
  const indeterminate = job.phase === 'running' && total <= 0;
  const ratio = total > 0 ? Math.max(0, Math.min(1, current / total)) : 0;
  const arcLength = Math.max(2, Math.round(ratio * 100));

  return (
    <DropdownMenu defaultOpen={defaultOpen}>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label="Server admin"
          className={styles.gearTrigger}
          data-state={showRing ? 'running' : 'idle'}
          data-indeterminate={indeterminate || undefined}
        >
          <span className={styles.gearGlyph} aria-hidden="true">
            <MaterialIcon name="settings" size={18} />
          </span>
          <span className={styles.gearRing} aria-hidden="true">
            <svg viewBox="0 0 18 18" role="presentation">
              <circle cx="9" cy="9" r="7" className={styles.ringTrack} />
              <circle
                cx="9"
                cy="9"
                r="7"
                className={styles.ringArc}
                pathLength={100}
                strokeDasharray={indeterminate ? undefined : `${arcLength} 100`}
                transform="rotate(-90 9 9)"
              />
            </svg>
          </span>
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className={styles.menu} sideOffset={8}>
        <div className={styles.menuHeader}>
          <Logo />
          <div className={styles.menuHeadline}>
            <span className={styles.menuHeadlineName}>kura · home server</span>
            <span className={styles.menuHeadlineSub}>library admin</span>
          </div>
        </div>

        {runningView ? (
          <RunningSection
            kind={job.kind ?? 'scan'}
            current={current}
            total={total}
            message={job.progress?.message ?? ''}
            indeterminate={indeterminate}
            ratio={ratio}
          />
        ) : (
          <div className={styles.menuSection}>
            <div className={styles.menuEyebrow}>Library</div>
            <button type="button" className={styles.menuItem} onClick={() => job.startScan()}>
              <span className={styles.menuItemIcon}>
                <MaterialIcon name="search" size={14} />
              </span>
              <span className={styles.menuItemBody}>
                <span className={styles.menuItemLabel}>Scan library</span>
                <span className={styles.menuItemSub}>find new files</span>
              </span>
            </button>
            <button type="button" className={styles.menuItem} onClick={() => job.startReindex()}>
              <span className={styles.menuItemIcon}>
                <MaterialIcon name="refresh" size={14} />
              </span>
              <span className={styles.menuItemBody}>
                <span className={styles.menuItemLabel}>Rebuild index</span>
                <span className={styles.menuItemSub}>re-read every file</span>
              </span>
            </button>
          </div>
        )}

        <div className={styles.menuSection}>
          <div className={styles.menuEyebrow}>Appearance</div>
          {THEME_OPTIONS.map((opt) => (
            <button
              key={opt.value}
              type="button"
              className={styles.menuItem}
              onClick={() => setTheme(opt.value)}
            >
              <span className={styles.menuItemIcon}>
                <MaterialIcon name={opt.icon} size={14} />
              </span>
              <span className={styles.menuItemBody}>
                <span className={styles.menuItemLabel}>{opt.label}</span>
                {opt.sub ? <span className={styles.menuItemSub}>{opt.sub}</span> : null}
              </span>
              <span className={styles.menuItemCheck} aria-hidden="true">
                {theme === opt.value ? '✓' : ''}
              </span>
            </button>
          ))}
        </div>

        {job.lastError ? <div className={styles.errorBanner}>{job.lastError.message}</div> : null}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

interface RunningSectionProps {
  kind: 'scan' | 'reindex';
  current: number;
  total: number;
  message: string;
  indeterminate: boolean;
  ratio: number;
}

function RunningSection({
  kind,
  current,
  total,
  message,
  indeterminate,
  ratio,
}: RunningSectionProps) {
  const title = kind === 'reindex' ? 'Rebuilding index' : 'Scanning library';
  return (
    <div
      className={styles.menuRunning}
      data-indeterminate={indeterminate || undefined}
      style={{ ['--p' as string]: ratio }}
    >
      <div className={styles.runningTitle}>{title}</div>
      <div className={styles.runningBar}>
        <div className={styles.runningBarFill} />
      </div>
      <div className={styles.runningMeta}>
        <span className={styles.runningPath}>{message || '…'}</span>
        <span className={styles.runningCounter}>
          {indeterminate ? `${current}` : `${current} / ${total}`}
        </span>
      </div>
    </div>
  );
}
