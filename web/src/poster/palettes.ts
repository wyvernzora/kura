/**
 * Poster art palettes — hand-picked, hand-printed feel. Tuple ordering
 * is fixed: [background, accent, ink, paper]. Compositions consume the
 * tuple positionally.
 */
export type Palette = readonly [bg: string, accent: string, ink: string, paper: string];

export const PALETTES: readonly Palette[] = [
  ['#3a78c5', '#e8b53a', '#1a1a1a', '#fafaf7'], // blue + mustard
  ['#c5483a', '#fafaf7', '#1a1a1a', '#fdebcd'], // red + cream
  ['#3a9a4a', '#1a1a1a', '#fafaf7', '#e8b53a'], // green + black
  ['#1a1a1a', '#e8b53a', '#fafaf7', '#c5483a'], // black + gold
  ['#dd9a3c', '#3a78c5', '#1a1a1a', '#fafaf7'], // ochre + indigo
  ['#5a4a8a', '#e8b53a', '#fafaf7', '#fafaf7'], // violet + gold
  ['#2a5550', '#dd6a4a', '#fafaf7', '#e8b53a'], // teal + coral
  ['#a6c5a0', '#1a1a1a', '#1a1a1a', '#fafaf7'], // sage + ink
];
