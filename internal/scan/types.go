package scan

import (
	"fmt"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
)

type Input struct {
	// Refresh forces every active record's mediainfo and source
	// detection to re-run, overwriting fields like resolution / codec
	// / size with the latest probe even when the file's mtime + size
	// are unchanged. Source merging is one-way: a freshly detected
	// "Unknown" never overwrites an existing non-Unknown source, so
	// callers can re-run scan to fix bad inferences without losing
	// good ones.
	Refresh bool
	// MetadataOnly skips the filesystem walk + mediainfo probes. The
	// scan still pulls the provider's spine + artwork + alias data
	// and recomputes searchKey, but no active records are touched.
	// Useful for cheap library-wide refreshes after a metadata-shape
	// change (e.g. searchKey recipe tweak) without re-probing every
	// file. Mutually exclusive with Refresh in spirit (MetadataOnly
	// short-circuits before Refresh would matter).
	MetadataOnly bool
	// Ordering, when non-empty, overwrites the series's persisted spine
	// ordering before the provider fetch. Empty leaves model.Ordering
	// untouched (re-spines under whatever the series was last pinned to,
	// or the provider default if never pinned). Validation is the caller's
	// responsibility — Runner forwards the value verbatim to the provider.
	Ordering string
	// Mutator stamps the series.json write at the end of a successful
	// scan. Required.
	Mutator coord.Mutator
}

type Result struct {
	Series      refs.Series      `json:"series"`
	Synced      []ScannedEpisode `json:"synced"`
	Skipped     []ImportSkip     `json:"skipped"`
	OrphanSlots []refs.Episode   `json:"orphanSlots"`
}

type ImportSkip struct {
	Path   string `json:"path"`
	Code   string `json:"code"`
	Reason string `json:"reason"`

	// Quality hints, populated when the skipped path is a recognized
	// video file (e.g. duplicate_slot entries). Other skip codes leave
	// these zero. Source/Resolution come from the filename suffix; Size
	// is the on-disk byte count. Sufficient signal for callers picking
	// a winner among duplicates.
	Source     string `json:"source,omitempty"`
	Resolution string `json:"resolution,omitempty"`
	Size       int64  `json:"size,omitempty"`
}

const (
	SkipCodeSpecialNumberNotInferred = "special_number_not_inferred"
	SkipCodeEpisodeNumberNotInferred = "episode_number_not_inferred"
	SkipCodeSeasonMismatch           = "season_mismatch"
	SkipCodeIgnoredDirectory         = "ignored_directory"
	SkipCodeDuplicateSlot            = "duplicate_slot"
	// SkipCodeMetadataSlotMissing: filename inferred to a season+
	// episode slot the provider's spine has no entry for (e.g.
	// "S01E11" where the provider has only 10 episodes in S01).
	// Soft skip; operator decides whether to stage to a different
	// slot, rename, or remove.
	SkipCodeMetadataSlotMissing = "metadata_slot_missing"
)

type ScannedEpisode struct {
	Status     ScanStatus   `json:"status"`
	Episode    refs.Episode `json:"episode"`
	Source     string       `json:"source"`
	Resolution string       `json:"resolution,omitempty"`
	Path       string       `json:"path"`
	Companions []string     `json:"companions"`
}

type ScanStatus string

const (
	ScanStatusAdded     ScanStatus = "added"
	ScanStatusUpdated   ScanStatus = "updated"
	ScanStatusUnchanged ScanStatus = "unchanged"
	ScanStatusRemoved   ScanStatus = "removed"
)

type EpisodeAlreadyExistsError struct {
	Episode refs.Episode
}

func (err EpisodeAlreadyExistsError) Error() string {
	return fmt.Sprintf("episode %s already has an active record at a different path; use kura_stage with replace=true to install the new file", err.Episode.Marker())
}
