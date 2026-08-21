import type { Candidate } from '@/api/types';

/**
 * Whether a TVDB search candidate looks like anime, for the
 * Settings → Search "Anime only" filter.
 *
 * TVDB tags most anime with a literal "Anime" genre, but some entries
 * only carry "Animation"; those count when the original language is
 * Japanese (the server normalizes TVDB's "jpn" to BCP-47 "ja", the
 * raw form is tolerated as insurance). Candidates without genre data
 * fail the check — the label promises "only anime", and the toggle
 * itself is the escape hatch when TVDB's metadata is too sparse.
 */
export function isAnimeCandidate(candidate: Candidate): boolean {
  const genres = (candidate.genres ?? []).map((g) => g.toLowerCase());
  if (genres.includes('anime')) {
    return true;
  }
  return (
    genres.includes('animation') &&
    (candidate.originalLanguage === 'ja' || candidate.originalLanguage === 'jpn')
  );
}
