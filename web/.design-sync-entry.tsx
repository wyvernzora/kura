// Bundle entry for /design-sync (claude.ai/design).
//
// This app has no library build, so the design-sync converter has no dist
// entry to bundle into `window.KuraWeb`. This barrel is that entry: it
// re-exports every storied component so the converter can expose them on the
// global, and the storybook-shape preview compiler can redirect each story's
// component import to the shipped bundle. Helper modules (_storyProviders,
// _fixtures, _storyRouter, …) are deliberately NOT exported — they must
// compile into each preview from source, not resolve to the global.
//
// Committed so re-syncs reuse it. Not imported by the app itself.

export * from '@/components/AppShell';
export * from '@/components/ClearFiltersButton';
export * from '@/components/ErrorScreen';
export * from '@/components/GearMenu';
export * from '@/components/LoginScreen';
export * from '@/components/Logo';
export * from '@/components/SearchField';
export * from '@/components/SortDropdown';
export * from '@/components/Splash';
export * from '@/components/StatusFilterDropdown';
export * from '@/components/TopBar';
export * from '@/components/ValueFilterDropdown';
export * from '@/components/VirtualPosterGrid';

export * from '@/components/series/CompanionsBadge';
export * from '@/components/series/EpisodeRow';
export * from '@/components/series/QualityChip';
export * from '@/components/series/ScanButton';
export * from '@/components/series/ScanDetailsModal';
export * from '@/components/series/SeasonPanel';
export * from '@/components/series/SeriesDetail';
export * from '@/components/series/SeriesDetailSkeleton';
export * from '@/components/series/SeriesPosterCard';
export * from '@/components/series/SeriesStatusCornerPill';
export * from '@/components/series/StatusDot';

export * from '@/components/ui/card';
export * from '@/components/ui/icon-btn';
export * from '@/components/ui/material-icon';
export * from '@/components/ui/poster';
export * from '@/components/ui/status-chip';
export * from '@/components/ui/surface';

export * from '@/poster/PosterArt';

// Story providers, exported so cfg.provider can wrap every preview in the
// SAME bundle's QueryClient/router/auth instances the components use. Bundling
// them separately (as story-helper imports) gives a second react-query
// instance whose context the components' hooks never see — leaving
// provider-coupled components (ScanButton, SeriesDetail, …) blank.
export { StoryProviders } from '@/components/_storyProviders';
export { StoryRouter } from '@/components/_storyRouter';

// Re-export react-query so cfg.storyImports.shim can redirect each story's
// `@tanstack/react-query` import to THIS bundle's single instance. Stories that
// seed the query cache (SeriesDetail's withSeededShow) otherwise seed a second,
// preview-only instance the components never read — leaving them stuck on the
// fetch/404 branch.
export * from '@tanstack/react-query';
