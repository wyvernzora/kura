package workflow

import (
	"context"
	"errors"
	"os"
	"sort"
	"time"

	"cloud.google.com/go/civil"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	domainseries "github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

// ShowInput parameters for the Show workflow. Filter fields compose
// AND across axes; empty value on an axis = no filter on that axis.
//
// Episodes is the parsed selector; transports parse the raw string at
// their decode boundary so malformed input rejects before any series
// load. Status / Source / Resolution are sets; an empty slice means
// "any."
type ShowInput struct {
	Ref        refs.Series
	Now        time.Time
	Episodes   refs.EpisodeSelector
	Status     []response.Status
	Source     []string
	Resolution []string
}

// Show returns the full observed state for one series: persisted
// metadata joined with computed per-episode status. Pure read; no fs
// probing. Drift between scans (a tracked active file going missing on
// disk) is the responsibility of `kura scan`, which prunes missing
// actives on the next walk; until then Show renders the persisted view.
func Show(ctx context.Context, deps Deps, in ShowInput) (response.Show, error) {
	if err := ctx.Err(); err != nil {
		return response.Show{}, err
	}
	if in.Ref.IsZero() {
		return response.Show{}, &NotFoundError{Ref: in.Ref}
	}
	model, err := seriesfile.Load(deps.LibRoot, in.Ref)
	if err != nil {
		// Surface a typed not-found rather than leaking the raw
		// os.ErrNotExist with its on-disk path; transports map this
		// to a 404 with a clean "series not tracked" message.
		if errors.Is(err, os.ErrNotExist) {
			return response.Show{}, &SeriesNotFoundError{Ref: in.Ref}
		}
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
	seriesRoot := paths.SeriesDir(deps.LibRoot, in.Ref)
	selector := in.Episodes
	if selector.Active {
		// Loud rejection when the season doesn't exist in the spine.
		// Empty range overlap (start..end vs available episode numbers)
		// stays quiet — handled by buildSeasons's filter dropping
		// rows.
		seen := false
		for ref := range model.Episodes {
			if ref.Season() == selector.Season {
				seen = true
				break
			}
		}
		if !seen {
			return response.Show{}, &EpisodeSelectorSeasonMissingError{
				Ref:      in.Ref,
				Selector: selector.String(),
				Season:   selector.Season,
			}
		}
	}
	filter := episodeFilter{
		selector:    selector,
		statuses:    statusSet(in.Status),
		sources:     stringSet(in.Source),
		resolutions: stringSet(in.Resolution),
	}
	row := indexfile.BuildRowFromModel(model, now)
	out := response.Show{
		MetadataRef:    model.Metadata,
		Ref:            in.Ref,
		Root:           seriesRoot,
		LastScanned:    formatOptionalTime(model.LastScanned),
		PreferredTitle: preferredTitle,
		CanonicalTitle: model.CanonicalTitle.String(),
		Status:         row.Status,
		IsAiring:       row.IsAiring,
		Seasons:        buildSeasons(seriesRoot, model, now, filter),
		StagedTrash:    buildStagedTrash(seriesRoot, model.StagedTrash),
		StagedExtras:   buildStagedExtras(model.StagedExtras),
	}
	if !model.Artwork.IsZero() {
		artwork := &response.ArtworkShow{}
		if !model.Artwork.Poster.IsZero() {
			artwork.Poster = &response.PosterShow{
				URL:          model.Artwork.Poster.URL,
				ThumbnailURL: model.Artwork.Poster.ThumbnailURL,
				Language:     model.Artwork.Poster.Language,
			}
		}
		out.Artwork = artwork
	}
	return out, nil
}

func buildStagedTrash(seriesRoot string, items []domainseries.StagedTrashItem) []response.TrashItemShow {
	if len(items) == 0 {
		return nil
	}
	sorted := make([]domainseries.StagedTrashItem, len(items))
	copy(sorted, items)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID.Compare(sorted[j].ID) < 0 })
	out := make([]response.TrashItemShow, 0, len(sorted))
	for _, item := range sorted {
		companions := make([]response.CompanionShow, 0, len(item.Companions))
		for _, c := range item.Companions {
			companions = append(companions, response.CompanionShow{
				Path:     relativeToSeries(seriesRoot, c.Path),
				Role:     c.Role,
				Language: c.Language,
				Label:    c.Label,
				Size:     c.Size,
				MTime:    c.MTime.UTC().Format(time.RFC3339),
			})
		}
		out = append(out, response.TrashItemShow{
			ID:         item.ID.String(),
			Path:       relativeToSeries(seriesRoot, item.Path),
			Size:       item.Size,
			MTime:      item.MTime.UTC().Format(time.RFC3339),
			AddedAt:    formatOptionalTime(item.AddedAt),
			Companions: companions,
		})
	}
	return out
}

func buildStagedExtras(items []domainseries.StagedExtraItem) []response.ExtraItemShow {
	if len(items) == 0 {
		return nil
	}
	sorted := make([]domainseries.StagedExtraItem, len(items))
	copy(sorted, items)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID.Compare(sorted[j].ID) < 0 })
	out := make([]response.ExtraItemShow, 0, len(sorted))
	for _, item := range sorted {
		out = append(out, response.ExtraItemShow{
			ID:      item.ID.String(),
			Season:  item.Season,
			Path:    item.Path,
			Prefix:  item.Prefix,
			IsDir:   item.IsDir,
			AddedAt: formatOptionalTime(item.AddedAt),
		})
	}
	return out
}

// episodeFilter applies the four ShowInput filter axes (episodes
// selector, status set, source set, resolution set) at episode-grouping
// time. Empty fields = no filter on that axis.
type episodeFilter struct {
	selector    refs.EpisodeSelector
	statuses    map[response.Status]struct{}
	sources     map[string]struct{}
	resolutions map[string]struct{}
}

func (f episodeFilter) match(ref refs.Episode, view response.EpisodeShow) bool {
	if f.selector.Active && !f.selector.Matches(ref) {
		return false
	}
	if len(f.statuses) > 0 {
		if _, ok := f.statuses[view.Status]; !ok {
			return false
		}
	}
	if len(f.sources) > 0 {
		if view.Active == nil {
			return false
		}
		if _, ok := f.sources[view.Active.Source]; !ok {
			return false
		}
	}
	if len(f.resolutions) > 0 {
		if view.Active == nil {
			return false
		}
		if _, ok := f.resolutions[view.Active.Resolution]; !ok {
			return false
		}
	}
	return true
}

func statusSet(in []response.Status) map[response.Status]struct{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[response.Status]struct{}, len(in))
	for _, s := range in {
		out[s] = struct{}{}
	}
	return out
}

func stringSet(in []string) map[string]struct{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(in))
	for _, s := range in {
		out[s] = struct{}{}
	}
	return out
}

func buildSeasons(seriesRoot string, model *domainseries.Series, now time.Time, filter episodeFilter) []response.SeasonShow {
	bySeason := map[int][]response.EpisodeShow{}
	allSeasons := map[int]struct{}{}
	for ref, episode := range model.Episodes {
		allSeasons[ref.Season()] = struct{}{}
		view := response.EpisodeShow{
			Episode:        ref,
			Aired:          formatAirDate(episode.AirDate),
			Status:         computeEpisodeStatus(episode, now),
			PreferredTitle: episode.PreferredTitle.String(),
			CanonicalTitle: episode.CanonicalTitle.String(),
		}
		if episode.Active != nil {
			m := mediaShow(seriesRoot, *episode.Active)
			view.Active = &m
		}
		if episode.Staged != nil {
			m := mediaShow(seriesRoot, *episode.Staged)
			view.Staged = &m
		}
		if !filter.match(ref, view) {
			continue
		}
		bySeason[ref.Season()] = append(bySeason[ref.Season()], view)
	}
	// Surface every season the filter would visit (selector.Active
	// scopes to one season; otherwise surface every season present in
	// the spine even if filtering left it empty so the caller sees
	// "this season exists, filter trimmed it to zero").
	if filter.selector.Active {
		// Only the selected season; verified to exist by caller.
		if _, ok := bySeason[filter.selector.Season]; !ok {
			bySeason[filter.selector.Season] = nil
		}
	} else {
		for s := range allSeasons {
			if _, ok := bySeason[s]; !ok {
				bySeason[s] = nil
			}
		}
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
		out = append(out, response.SeasonShow{
			Number:   n,
			Summary:  summarizeSeason(eps),
			Episodes: eps,
		})
	}
	return out
}

func summarizeSeason(eps []response.EpisodeShow) response.SeasonSummary {
	out := response.SeasonSummary{EpisodeCount: len(eps)}
	for _, ep := range eps {
		switch ep.Status {
		case response.StatusPresent:
			out.Present++
		case response.StatusMissing:
			out.Missing++
		case response.StatusStaged:
			out.Staged++
		case response.StatusStagedReplacement:
			out.StagedReplacement++
		case response.StatusPending:
			out.Pending++
		}
	}
	return out
}

func computeEpisodeStatus(episode domainseries.Episode, now time.Time) response.Status {
	if episode.Active != nil && episode.Staged != nil {
		return response.StatusStagedReplacement
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

func mediaShow(seriesRoot string, record media.Record) response.MediaShow {
	companions := make([]response.CompanionShow, 0, len(record.Companions))
	for _, c := range record.Companions {
		companions = append(companions, response.CompanionShow{
			Path:     relativeToSeries(seriesRoot, c.Path),
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
		File:       relativeToSeries(seriesRoot, record.Path),
		Companions: companions,
	}
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func isPending(aired civil.Date, now time.Time) bool {
	if !aired.IsValid() {
		return false
	}
	return aired.After(civil.DateOf(now))
}

func formatAirDate(value civil.Date) string {
	if !value.IsValid() {
		return ""
	}
	return value.String()
}
