package series

import (
	"context"
	"errors"
	"fmt"

	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/refs"
)

type ScanInput struct {
	Replace bool
}

type ScanResult struct {
	Series  refs.Series      `json:"series"`
	Synced  []ScannedEpisode `json:"synced"`
	Skipped []ImportSkip     `json:"skipped"`
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

func (h Handle) Scan(ctx context.Context, in ScanInput) (ScanResult, error) {
	scanner := newScanner(h, ctx, in)
	if err := scanner.scan(); err != nil {
		return ScanResult{}, err
	}
	return scanner.result, nil
}

func spineFromMetadata(seasons []metadata.Season) ([]SpineEpisode, error) {
	var spine []SpineEpisode
	for _, season := range seasons {
		for _, episode := range season.Episodes {
			if episode.Ref.IsZero() {
				return nil, fmt.Errorf("series: metadata has invalid episode ref")
			}
			spine = append(spine, SpineEpisode{Ref: episode.Ref, AirDate: episode.Aired})
		}
	}
	return spine, nil
}

func IsNotTracked(err error) bool {
	var notTracked SeriesNotTrackedError
	return errors.As(err, &notTracked)
}
