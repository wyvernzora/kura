package series

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/refs"
)

type ScanInput struct {
	Replace bool
}

type ScanResult struct {
	Series  refs.Series
	Synced  []ScannedEpisode
	Skipped []fsroot.ImportSkip
}

type ScannedEpisode struct {
	Status     ScanStatus
	Season     int
	Special    bool
	Number     int
	Source     string
	Resolution string
	Path       string
	Companions []string
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
	series, err := h.Load()
	if err != nil {
		return ScanResult{}, err
	}
	metadataSeries, err := h.source().GetSeries(ctx, series.Metadata.ID())
	if err != nil {
		return ScanResult{}, err
	}
	spine, err := spineFromMetadata(metadataSeries.Seasons)
	if err != nil {
		return ScanResult{}, err
	}
	editor := editor{series: &series}
	editor.refreshSpine(spine)

	discovered, skipped, err := h.files().discover(h.ref)
	if err != nil {
		return ScanResult{}, err
	}
	seriesDir, err := h.files().seriesDir(h.ref)
	if err != nil {
		return ScanResult{}, err
	}

	result := ScanResult{Series: h.ref, Skipped: skipped}
	for _, file := range discovered {
		episode, ok := series.Episodes[file.Ref]
		if !ok {
			return ScanResult{}, MetadataMissingEpisodeError{Season: file.Ref.Season(), Episode: file.Ref.Episode()}
		}
		status := ScanStatusNew
		if episode.Active != nil {
			if episode.Active.Path != file.Path {
				if !in.Replace {
					return ScanResult{}, EpisodeAlreadyExistsError{Season: file.Ref.Season(), Episode: file.Ref.Episode()}
				}
				status = ScanStatusReplaced
			} else if unchanged, err := h.unchanged(seriesDir, *episode.Active, file); err != nil {
				return ScanResult{}, err
			} else if unchanged {
				result.Synced = append(result.Synced, scanEntry(statusExisting(file, *episode.Active)))
				continue
			} else {
				status = ScanStatusUpdated
			}
		}
		record, err := h.mediaRecord(ctx, seriesDir, file)
		if err != nil {
			return ScanResult{}, err
		}
		if err := editor.setActive(file.Ref, record); err != nil {
			return ScanResult{}, err
		}
		result.Synced = append(result.Synced, ScannedEpisode{
			Status:     status,
			Season:     file.Ref.Season(),
			Special:    file.Ref.Season() == 0,
			Number:     file.Ref.Episode(),
			Source:     domain.ParseMediaSource(record.Source).Display(),
			Resolution: record.Resolution,
			Path:       record.Path,
			Companions: append([]string(nil), file.Companions...),
		})
	}
	series.LastScanned = h.now().UTC()
	if err := h.repo().save(h.ref, series); err != nil {
		return ScanResult{}, err
	}
	return result, nil
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

func (h Handle) unchanged(seriesDir fsroot.SeriesDir, active MediaRecord, file discoveredFile) (bool, error) {
	facts, err := h.files().stat(filepath.Join(seriesDir.Path(), filepath.FromSlash(file.Path)))
	if err != nil {
		return false, err
	}
	if active.Size != facts.Size || !active.MTime.Equal(facts.MTime) {
		return false, nil
	}
	if len(active.Companions) != len(file.Companions) {
		return false, nil
	}
	companions := map[string]CompanionRecord{}
	for _, companion := range active.Companions {
		companions[companion.Path] = companion
	}
	for _, path := range file.Companions {
		companion, ok := companions[path]
		if !ok {
			return false, nil
		}
		facts, err := h.files().stat(filepath.Join(seriesDir.Path(), filepath.FromSlash(path)))
		if err != nil {
			return false, nil
		}
		if companion.Size != facts.Size || !companion.MTime.Equal(facts.MTime) {
			return false, nil
		}
	}
	return true, nil
}

func (h Handle) mediaRecord(ctx context.Context, seriesDir fsroot.SeriesDir, file discoveredFile) (MediaRecord, error) {
	absolutePath := filepath.Join(seriesDir.Path(), filepath.FromSlash(file.Path))
	info, err := h.inspector().Inspect(ctx, absolutePath)
	if err != nil {
		return MediaRecord{}, err
	}
	facts, err := h.files().stat(absolutePath)
	if err != nil {
		return MediaRecord{}, err
	}
	record := MediaRecord{
		Path:       file.Path,
		Source:     domain.ParseMediaSource(file.Source).String(),
		Resolution: info.Resolution,
		Codec:      info.VideoCodec,
		Size:       facts.Size,
		MTime:      facts.MTime,
		Companions: []CompanionRecord{},
	}
	for _, companionPath := range file.Companions {
		facts, err := h.files().stat(filepath.Join(seriesDir.Path(), filepath.FromSlash(companionPath)))
		if err != nil {
			return MediaRecord{}, err
		}
		record.Companions = append(record.Companions, CompanionRecord{
			Path:  companionPath,
			Size:  facts.Size,
			MTime: facts.MTime,
		})
	}
	return record, nil
}

func statusExisting(file discoveredFile, active MediaRecord) ScannedEpisode {
	return ScannedEpisode{
		Status:     ScanStatusExisting,
		Season:     file.Ref.Season(),
		Special:    file.Ref.Season() == 0,
		Number:     file.Ref.Episode(),
		Source:     domain.ParseMediaSource(active.Source).Display(),
		Resolution: active.Resolution,
		Path:       active.Path,
		Companions: append([]string(nil), file.Companions...),
	}
}

func scanEntry(entry ScannedEpisode) ScannedEpisode {
	return entry
}

func IsNotTracked(err error) bool {
	var notTracked SeriesNotTrackedError
	return errors.As(err, &notTracked)
}
