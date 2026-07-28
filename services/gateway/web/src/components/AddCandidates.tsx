import { useNavigate } from '@tanstack/react-router';
import { ChevronDown } from 'lucide-react';
import { useState } from 'react';

import { useResolveSearch } from '@/api/hooks';
import type { Candidate } from '@/api/types';
import { Poster } from '@/components/ui/poster';
import { cn } from '@/lib/cn';

/**
 * Minimum query length before we hit TVDB. Higher than the resolve
 * hook's own floor (2) so the home page's "find-or-add" section only
 * fires on reasonably specific queries — the "narrow enough" guard.
 */
const MIN_QUERY_LENGTH = 3;

interface AddCandidatesProps {
  /** The active (trimmed upstream) search query. */
  query: string;
  /** Metadata refs already in the library, for dedupe. */
  libraryRefs: ReadonlySet<string>;
}

/**
 * Collapsible "Not in your library" panel above the home search
 * results. Resolves the query against the provider, drops candidates
 * already in the library, and renders the rest as poster cards.
 * Clicking a card opens that series' detail page in preview mode
 * (`/series/{ref}?preview=true`, served from live provider metadata),
 * where the user confirms the add.
 *
 * Renders nothing until the query is long enough and there is at least
 * one addable candidate (or a fetch in flight), so the browse view is
 * untouched for short / already-owned queries.
 */
export function AddCandidates({ query, libraryRefs }: AddCandidatesProps) {
  const trimmed = query.trim();
  const enabled = trimmed.length >= MIN_QUERY_LENGTH;
  // Pass '' when below the floor so the hook stays disabled.
  const resolve = useResolveSearch(enabled ? trimmed : '');
  const navigate = useNavigate();
  const [open, setOpen] = useState(true);

  const candidates = (resolve.data?.candidates ?? []).filter((c) => !libraryRefs.has(c.ref));

  if (!enabled) {
    return null;
  }
  // Nothing to add and nothing loading → stay invisible rather than
  // flash an empty panel at the top of the results.
  if (candidates.length === 0 && !resolve.isFetching) {
    return null;
  }

  const onPick = (c: Candidate) => {
    void navigate({ to: '/series/$ref', params: { ref: c.ref }, search: { preview: true } });
  };

  return (
    <section className="mb-6 rounded-[14px] border border-line-soft bg-surface p-4 shadow-card">
      <header className="flex items-center gap-3">
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          className="kura-focusable-kb flex items-center gap-2 rounded text-sm font-semibold tracking-tight text-ink"
        >
          <ChevronDown
            aria-hidden="true"
            className={cn('h-4 w-4 transition-transform', !open && '-rotate-90')}
          />
          Not in your library
        </button>
        {candidates.length > 0 && <span className="text-xs text-muted">{candidates.length}</span>}
        {resolve.isFetching && <span className="text-xs text-muted">searching TVDB…</span>}
      </header>

      {open && candidates.length > 0 && (
        <div className="mt-4 grid gap-3 [grid-template-columns:repeat(auto-fill,minmax(140px,1fr))]">
          {candidates.map((c) => (
            <Poster
              key={c.ref}
              title={c.year ? `${c.preferredTitle} (${c.year})` : c.preferredTitle}
              // Neutral status: enables the image render path with no
              // status badges (these aren't tracked series yet).
              status="complete"
              posterUrl={c.posterUrl}
              posterThumbnailUrl={c.posterThumbnailUrl}
              onClick={() => onPick(c)}
            />
          ))}
        </div>
      )}
    </section>
  );
}
