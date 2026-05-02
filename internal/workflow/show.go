package workflow

import (
	"context"
	"sort"
	"time"

	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	domainseries "github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/series/layout"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/seriesdir"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

// ShowInput parameters for the Show workflow.
type ShowInput struct {
	Ref refs.Series
	Now time.Time
}

// Show returns the full observed state for one series: persisted
// metadata joined with computed per-episode status and filesystem-issue
// lists. Pure read; no mutations.
func Show(ctx context.Context, deps Deps, in ShowInput) (response.Show, error) {
	_ = ctx
	if in.Ref.IsZero() {
		return response.Show{}, &NotFoundError{Ref: in.Ref}
	}
	model, err := seriesfile.Load(deps.LibRoot, in.Ref)
	if err != nil {
		return response.Show{}, err
	}
	now := in.Now
	if now.IsZero() {
		now = deps.Now()
	}
	preferredTitle := model.PreferredTitle.String()
	if preferredTitle == "" {
		preferredTitle = in.Ref.String()
	}
	seriesDir, err := seriesdir.Parse(paths.SeriesDir(deps.LibRoot, in.Ref))
	if err != nil {
		return response.Show{}, err
	}
	return response.Show{
		MetadataRef:    model.Metadata,
		Ref:            in.Ref,
		Root:           paths.SeriesDir(deps.LibRoot, in.Ref),
		LastScanned:    formatOptionalTime(model.LastScanned),
		PreferredTitle: preferredTitle,
		CanonicalTitle: model.CanonicalTitle.String(),
		Seasons:        buildSeasons(seriesDir, model, now),
	}, nil
}

func buildSeasons(seriesDir seriesdir.SeriesDir, model *domainseries.Series, now time.Time) []response.SeasonShow {
	bySeason := map[int][]response.EpisodeShow{}
	for ref, episode := range model.Episodes {
		view := response.EpisodeShow{
			Episode:         ref,
			Aired:           formatAirDate(episode.AirDate),
			Status:          computeEpisodeStatus(seriesDir, episode, now),
			Inconsistencies: episodeIssues(seriesDir, episode),
		}
		if episode.Active != nil {
			m := mediaShow(*episode.Active)
			view.Active = &m
		}
		if episode.Staged != nil {
			m := mediaShow(*episode.Staged)
			view.Staged = &m
		}
		bySeason[ref.Season()] = append(bySeason[ref.Season()], view)
	}
	numbers := make([]int, 0, len(bySeason))
	for n := range bySeason {
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)
	out := make([]response.SeasonShow, 0, len(numbers))
	for _, n := range numbers {
		eps := bySeason[n]
		sort.Slice(eps, func(i, j int) bool { return eps[i].Episode.Episode() < eps[j].Episode.Episode() })
		out = append(out, response.SeasonShow{Number: n, Episodes: eps})
	}
	return out
}

func computeEpisodeStatus(seriesDir seriesdir.SeriesDir, episode domainseries.Episode, now time.Time) response.Status {
	if episode.Active != nil && episode.Staged != nil {
		return response.StatusStagedReplacement
	}
	if episode.Active != nil && len(layout.PathFilesystemIssues(seriesDir, "active", "media", episode.Active.Path)) > 0 {
		return response.StatusUnavailable
	}
	if episode.Staged != nil {
		return response.StatusStaged
	}
	if episode.Active != nil {
		return response.StatusPresent
	}
	if isPending(episode.AirDate, now) {
		return response.StatusPending
	}
	return response.StatusMissing
}

func episodeIssues(seriesDir seriesdir.SeriesDir, episode domainseries.Episode) []response.Issue {
	raw := layout.EpisodeFilesystemIssues(seriesDir, episode)
	if len(raw) == 0 {
		return nil
	}
	out := make([]response.Issue, 0, len(raw))
	for _, issue := range raw {
		out = append(out, response.Issue{
			Record: issue.Record,
			Path:   issue.Path,
			Code:   issue.Code,
			Reason: issue.Reason,
		})
	}
	return out
}

func mediaShow(record media.Record) response.MediaShow {
	companions := make([]response.CompanionShow, 0, len(record.Companions))
	for _, c := range record.Companions {
		companions = append(companions, response.CompanionShow{
			Path:     c.Path,
			Role:     c.Role,
			Language: c.Language,
			Label:    c.Label,
			Size:     c.Size,
			MTime:    c.MTime.UTC().Format(time.RFC3339),
		})
	}
	return response.MediaShow{
		Source:     record.Source.Display(),
		Resolution: record.Resolution.Display(),
		Codec:      record.Codec.String(),
		Size:       record.Size,
		File:       record.Path,
		Companions: companions,
	}
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func formatAirDate(value interface {
	IsValid() bool
	String() string
}) string {
	if !value.IsValid() {
		return ""
	}
	return value.String()
}
