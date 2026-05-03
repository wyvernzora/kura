package scan

import (
	"fmt"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
)

type Input struct {
	Replace bool
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
}

const (
	SkipCodeSpecialNumberNotInferred = "special_number_not_inferred"
	SkipCodeEpisodeNumberNotInferred = "episode_number_not_inferred"
	SkipCodeSeasonMismatch           = "season_mismatch"
	SkipCodeIgnoredDirectory         = "ignored_directory"
	SkipCodeDuplicateSlot            = "duplicate_slot"
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
	ScanStatusReplaced  ScanStatus = "replaced"
	ScanStatusUpdated   ScanStatus = "updated"
	ScanStatusUnchanged ScanStatus = "unchanged"
	ScanStatusRemoved   ScanStatus = "removed"
)

type EpisodeAlreadyExistsError struct {
	Episode refs.Episode
}

func (err EpisodeAlreadyExistsError) Error() string {
	return fmt.Sprintf("episode %s already exists; pass replace to replace it", err.Episode.Marker())
}

type MetadataMissingEpisodeError struct {
	Episode refs.Episode
}

func (err MetadataMissingEpisodeError) Error() string {
	return fmt.Sprintf("metadata has no %s", err.Episode.Marker())
}

type ScanStagedRecordsError struct {
	Episodes []refs.Episode
}

func (err ScanStagedRecordsError) Error() string {
	if len(err.Episodes) == 1 {
		return fmt.Sprintf("series has staged episode %s; reconcile or reset staged records before scanning", err.Episodes[0].Marker())
	}
	return fmt.Sprintf("series has %d staged episodes; reconcile or reset staged records before scanning", len(err.Episodes))
}
