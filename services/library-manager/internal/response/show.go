package response

import "github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"

// Show is workflow.Show's full response: persisted series metadata
// joined with derived per-episode status and filesystem-issue lists.
//
// All path fields in this response (Root, MediaShow.File,
// CompanionShow.Path, TrashItemShow.Path, ExtraItemShow.Path, ...)
// are scheme-tagged selectors: `library:<rel>` for paths under
// KURA_LIBRARY_ROOT (Root emits as `library:<series-dir>`),
// `series:<rel>` for files inside the request's series root,
// `inbox:<rel>` for files under the inbox root. There are no raw
// filesystem paths.
type Show struct {
	MetadataRef    refs.Metadata `json:"metadataRef"`
	Ref            refs.Series   `json:"ref"`
	Root           string        `json:"root"`
	LastScanned    string        `json:"lastScanned,omitempty"`
	PreferredTitle string        `json:"preferredTitle"`
	CanonicalTitle string        `json:"canonicalTitle,omitempty"`
	Tags           []string      `json:"tags,omitempty"`
	Status         ListStatus    `json:"status"`
	// IsAiring mirrors ListRow.IsAiring — observed-airing flag
	// independent of Status.
	IsAiring        bool            `json:"isAiring,omitempty"`
	Artwork         *ArtworkShow    `json:"artwork,omitempty"`
	Seasons         []SeasonShow    `json:"seasons"`
	Truncated       bool            `json:"truncated,omitempty"`
	TruncatedRanges []string        `json:"truncatedRanges,omitempty"`
	TruncationHint  string          `json:"truncationHint,omitempty"`
	StagedTrash     []TrashItemShow `json:"stagedTrash,omitempty"`
	StagedExtras    []ExtraItemShow `json:"stagedExtras,omitempty"`
}

// TrashItemShow is one stagedTrash entry queued for removal at the next
// reconcile_apply. Path is a `series:<rel>` selector pointing at the
// original location. Companions list original locations alongside
// Path; trash bucket structure is intentionally not exposed on this
// surface.
type TrashItemShow struct {
	ID         string          `json:"id"`
	Path       string          `json:"path"`
	Size       int64           `json:"size"`
	MTime      string          `json:"mtime"`
	AddedAt    string          `json:"addedAt,omitempty"`
	Companions []CompanionShow `json:"companions,omitempty"`
}

// ExtraItemShow is one stagedExtras entry queued for placement under
// Season N/Extra/[Prefix]/<basename> at the next reconcile_apply.
// Path is an `inbox:<rel>` selector identifying the source.
type ExtraItemShow struct {
	ID      string `json:"id"`
	Season  int    `json:"season"`
	Path    string `json:"path"`
	Prefix  string `json:"prefix,omitempty"`
	IsDir   bool   `json:"isDir"`
	AddedAt string `json:"addedAt,omitempty"`
}

// SeasonShow groups episode rows by season number. Number 0 is specials.
// Summary always reflects the (possibly filtered) Episodes slice; clients
// reading filtered responses see filtered summaries, not series-wide totals.
type SeasonShow struct {
	Number   int           `json:"number"`
	Summary  SeasonSummary `json:"summary"`
	Episodes []EpisodeShow `json:"episodes,omitempty"`
}

// SeasonSummary is the per-season status rollup. Computed over the
// filter-applied Episodes slice; see SeasonShow doc.
type SeasonSummary struct {
	EpisodeCount      int `json:"episodeCount"`
	Present           int `json:"present"`
	Missing           int `json:"missing"`
	Staged            int `json:"staged"`
	StagedReplacement int `json:"stagedReplacement"`
	Pending           int `json:"pending"`
}

// EpisodeShow is one episode row with computed status and the active /
// staged media records (if any). Title fields are provider-supplied
// display names: PreferredTitle follows KURA_PREFERRED_LANGUAGES with
// fallback to canonical; CanonicalTitle is the provider's
// default-language form.
type EpisodeShow struct {
	Episode        refs.Episode `json:"episode"`
	Aired          string       `json:"aired,omitempty"`
	Status         Status       `json:"status"`
	PreferredTitle string       `json:"preferredTitle,omitempty"`
	CanonicalTitle string       `json:"canonicalTitle,omitempty"`
	Active         *MediaShow   `json:"active,omitempty"`
	Staged         *MediaShow   `json:"staged,omitempty"`
}

// ArtworkShow bundles series-level imagery surfaced in show responses.
// Today only Poster is populated; banner / fanart / clearlogo would
// add more fields without reshuffling consumers.
type ArtworkShow struct {
	Poster *PosterShow `json:"poster,omitempty"`
}

// PosterShow is the series-level poster URL.  URL only — kura does not
// cache image bytes.
type PosterShow struct {
	URL          string `json:"url"`
	ThumbnailURL string `json:"thumbnailUrl,omitempty"`
	Language     string `json:"language,omitempty"`
}

// MediaShow is one media file's display fields for the show view.
// File / Companions[].Path are scheme-tagged selectors:
// `series:<rel>` for files inside the series root, `inbox:<rel>` for
// inbox-staged files. Agents can pass them straight back to
// kura_stage / kura_trash without further parsing.
type MediaShow struct {
	Source     string            `json:"source"`
	Resolution string            `json:"resolution,omitempty"`
	Dimensions string            `json:"dimensions,omitempty"`
	Codec      string            `json:"codec,omitempty"`
	Size       int64             `json:"size"`
	MTime      string            `json:"mtime,omitempty"`
	File       string            `json:"file"`
	Companions []CompanionShow   `json:"companions"`
	Attrs      map[string]string `json:"attrs,omitempty"`
}

type CompanionShow struct {
	Path     string `json:"path"`
	Role     string `json:"role,omitempty"`
	Language string `json:"language,omitempty"`
	Label    string `json:"label,omitempty"`
	Size     int64  `json:"size"`
	MTime    string `json:"mtime"`
}
