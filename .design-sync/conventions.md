## Kura web — how to build with this design system

Kura is an anime-first library manager (Sonarr-category). These are real React 19
components compiled from the app, styled with **Tailwind CSS v4** and a custom
design-token scale. Build with them as-is; don't restyle from scratch.

### Setup — theme + providers

- **Theme** is set by a `data-k-theme` attribute on `<html>`: `paper` (warm-gray
  light, the default) or `dark` (deep-navy). All color tokens flip from this one
  attribute — set it once on the root, don't theme components individually.
- Most components are **presentational and need no wrapper**: `Card`, `Surface`,
  `Poster`, `PosterArt`, `StatusChip`, `StatusDot`, `MaterialIcon`, `IconBtn`,
  `CompanionsBadge`, `ResolutionChip`, `SeriesStatusCornerPill`, `ErrorScreen`,
  `LoginScreen`, `Splash`, `SearchField`, the `*Dropdown`s, `SeriesDetailSkeleton`.
- These need app context to function — wrap your tree in a **TanStack Router**
  context AND a react-query **`QueryClientProvider`**: `TopBar`, `AppShell`,
  `SeriesDetail`, `SeriesPosterCard`, `ScanButton`, `GearMenu`, `VirtualPosterGrid`,
  and `Logo` (when `interactive`). Without them these read `null` router/query
  state and render an error card.

### Styling idiom — Tailwind v4 utilities, custom token scale

Style with utility classes; the design language lives in these **custom tokens**
(use them, don't hard-code hex/px):

| Concern | Classes |
|---|---|
| Surfaces | `bg-paper` (page), `bg-surface` / `bg-surface-2` (cards), `bg-ink` + `text-paper` (inverted), `bg-overlay` / `bg-overlay-soft` |
| Text | `text-ink` (primary), `text-ink-2` (secondary), `text-muted` (tertiary) |
| Borders | `border-line`, `border-line-soft` |
| Status (tracking state) | `bg-status-airing` / `-complete` / `-incomplete` / `-untracked` / `-error`, each with a matching `text-status-<name>-fg` foreground |
| Elevation ("levitation") | `shadow-card`, `shadow-card-hover`, `shadow-pop`, `shadow-poster`, `shadow-poster-hover` |
| Radius | `rounded-sm` / `rounded-md` / `rounded-lg` |
| Type | `font-sans` (Inter — UI), `font-mono` (JetBrains Mono — IDs/codes like `tvdb:424536`, `S01E02`), `font-serif` (Newsreader) |
| Icons | render via the `MaterialIcon` component or `IconBtn` (Material Symbols Outlined), not raw `<i>` |

### Where the truth lives

Read `styles.css` (and the `@import`ed `_ds_bundle.css`) for the full token
definitions and both theme blocks, and each component's `<Name>.prompt.md` for
usage with real story-derived examples. Prefer reading those over guessing.

### One idiomatic example

```tsx
// A levitating card in the Kura idiom — library component + token utilities.
import { Card } from '<bundle>';

<div className="bg-paper p-6 font-sans" data-k-theme="paper">
  <Card className="bg-surface text-ink shadow-card rounded-md p-4 border border-line-soft">
    <div className="text-sm font-semibold">葬送のフリーレン</div>
    <div className="font-mono text-xs text-muted">tvdb:424536</div>
    <span className="bg-status-airing text-status-airing-fg rounded-full px-2 py-0.5 text-xs">
      AIRING
    </span>
  </Card>
</div>
```
