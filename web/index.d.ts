// Types entry for /design-sync (claude.ai/design) — DO NOT import in app code.
//
// This app has no library .d.ts tree. The design-sync converter scans a
// package types entry to (1) curate which storybook components are public and
// (2) extract prop bodies. This stub satisfies (1) by declaring every storied
// component name; prop bodies stay loose, so component API detail comes from
// the verified previews and the story-source examples in <Name>.prompt.md.
//
// Names are storybook title leaf segments (QualityChip -> ResolutionChip via
// cfg.titleMap). If you add/rename a storied component, update this file too
// (a re-sync flags drift as [TITLE_UNMAPPED]). Committed.

export declare const AppShell: (props: any) => any;
export declare const Card: (props: any) => any;
export declare const ClearFiltersButton: (props: any) => any;
export declare const CompanionsBadge: (props: any) => any;
export declare const EpisodeRow: (props: any) => any;
export declare const ErrorScreen: (props: any) => any;
export declare const GearMenu: (props: any) => any;
export declare const IconBtn: (props: any) => any;
export declare const LoginScreen: (props: any) => any;
export declare const Logo: (props: any) => any;
export declare const MaterialIcon: (props: any) => any;
export declare const Poster: (props: any) => any;
export declare const PosterArt: (props: any) => any;
export declare const ResolutionChip: (props: any) => any;
export declare const ScanButton: (props: any) => any;
export declare const ScanDetailsModal: (props: any) => any;
export declare const SearchField: (props: any) => any;
export declare const SeasonPanel: (props: any) => any;
export declare const SeriesDetail: (props: any) => any;
export declare const SeriesDetailSkeleton: (props: any) => any;
export declare const SeriesPosterCard: (props: any) => any;
export declare const SeriesStatusCornerPill: (props: any) => any;
export declare const SortDropdown: (props: any) => any;
export declare const Splash: (props: any) => any;
export declare const StatusChip: (props: any) => any;
export declare const StatusDot: (props: any) => any;
export declare const StatusFilterDropdown: (props: any) => any;
export declare const Surface: (props: any) => any;
export declare const TopBar: (props: any) => any;
export declare const ValueFilterDropdown: (props: any) => any;
export declare const VirtualPosterGrid: (props: any) => any;
