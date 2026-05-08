import { ChevronDown, ChevronRight } from 'lucide-react';
import { useState } from 'react';

import type { SeasonShow } from '@/api/types';
import { EpisodeRow } from '@/components/series/EpisodeRow';
import { cn } from '@/lib/cn';

interface SeasonPanelProps {
  season: SeasonShow;
  defaultOpen?: boolean;
  className?: string;
}

/**
 * One collapsible season block. Header shows label + present/total
 * rollup; clicking toggles the body. Specials (season 0) default to
 * closed so the most actionable seasons surface first.
 */
export function SeasonPanel({ season, defaultOpen, className }: SeasonPanelProps) {
  const isSpecials = season.number === 0;
  const [open, setOpen] = useState(defaultOpen ?? !isSpecials);
  const label = isSpecials ? 'Specials' : `Season ${season.number}`;
  const present = season.summary.present;
  const total = season.summary.episodeCount;
  const episodes = season.episodes ?? [];
  return (
    <div
      className={cn('mb-[18px] overflow-hidden rounded-[12px] bg-surface shadow-card', className)}
    >
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className={cn(
          'flex w-full items-center gap-2 px-[18px] py-[14px] text-left',
          // bg-surface-2 sits one notch off bg-surface (the body of
          // the panel) so the header reads as its own band without
          // adding lines or weight. Subtle in both palettes.
          'bg-surface-2',
          'transition-colors duration-[120ms] ease-out hover:bg-overlay-soft',
          open && 'border-line-soft border-b',
        )}
      >
        {open ? (
          <ChevronDown aria-hidden="true" className="h-3.5 w-3.5 text-muted" />
        ) : (
          <ChevronRight aria-hidden="true" className="h-3.5 w-3.5 text-muted" />
        )}
        <span className="font-mono text-[10px] font-bold tracking-[1.5px] text-ink uppercase">
          {label}
        </span>
        <span className="font-mono text-[10px] text-muted">
          · {present}/{total}
        </span>
      </button>
      {open && (
        <div>
          {episodes.length === 0 ? (
            <div className="px-[18px] py-3 font-mono text-[11px] text-muted">
              No episodes available.
            </div>
          ) : (
            episodes.map((ep) => <EpisodeRow key={ep.episode} episode={ep} />)
          )}
        </div>
      )}
    </div>
  );
}
