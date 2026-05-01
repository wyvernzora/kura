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
	Status     ScanStatus `json:"status"`
	Season     int        `json:"season,omitempty"`
	Special    bool       `json:"special,omitempty"`
	Number     int        `json:"number"`
	Source     string     `json:"source"`
	Resolution string     `json:"resolution,omitempty"`
	Path       string     `json:"path"`
	Companions []string   `json:"companions"`
}

type ScanStatus string

const (
	ScanStatusNew      ScanStatus = "new"
	ScanStatusReplaced ScanStatus = "replaced"
	ScanStatusUpdated  ScanStatus = "updated"
	ScanStatusExisting ScanStatus = "existing"
)

type EpisodeAlreadyExistsError struct {
	Season  int
	Episode int
}

func (err EpisodeAlreadyExistsError) Error() string {
	return fmt.Sprintf("episode S%02dE%02d already exists; pass replace to replace it", err.Season, err.Episode)
}

type MetadataMissingEpisodeError struct {
	Season  int
	Episode int
}

func (err MetadataMissingEpisodeError) Error() string {
	return fmt.Sprintf("metadata has no S%02dE%02d", err.Season, err.Episode)
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
			ref, err := refs.NewEpisode(episode.SeasonNumber, episode.EpisodeNumber)
			if err != nil {
				return nil, err
			}
			spine = append(spine, SpineEpisode{Ref: ref, AirDate: episode.Aired})
		}
	}
	return spine, nil
}

func IsNotTracked(err error) bool {
	var notTracked SeriesNotTrackedError
	return errors.As(err, &notTracked)
}
