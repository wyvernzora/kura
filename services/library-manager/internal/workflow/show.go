package workflow

import (
	"context"
	"errors"
	"os"
	"slices"
	"sort"
	"time"

	"cloud.google.com/go/civil"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/media"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	domainseries "github.com/wyvernzora/kura/services/library-manager/internal/domain/series"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/indexfile"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/paths"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
)

// ShowInput parameters for the Show workflow. Filter fields compose
// AND across axes; empty value on an axis = no filter on that axis.
//
// Episodes is the parsed selector; transports parse the raw string at
// their decode boundary so malformed input rejects before any series
// load. Selector keywords (NONE, AIRING_SEASON) replace only the
// episode axis and still compose with Status / Source / Resolution.
// Status / Source / Resolution are sets; an empty slice means "any."
type ShowInput struct {
	Ref refs.Series
	// MetadataRef + Preview drive the live-provider preview path: when
	// Preview is set, Show ignores Ref and builds the response from the
	// provider's metadata for MetadataRef instead of local series.json.
	// Used to render a series' detail page before it's added to the
	// library. All episodes report as missing.
	MetadataRef refs.Metadata
	Preview     bool
	Now         time.Time
	Episodes    refs.EpisodeSelector
	Status      []api.Status
	Source      []string
	Resolution  []string
}

// Show returns the full observed state for one series: persisted
// metadata joined with computed per-episode status. Pure read; no fs
// probing. Drift between scans (a tracked active file going missing on
// disk) is the responsibility of `kura scan`, which prunes missing
// actives on the next walk; until then Show renders the persisted view.
func Show(ctx context.Context, deps Deps, in ShowInput) (api.Show, error) {
	if err := ctx.Err(); err != nil {
		return api.Show{}, err
	}
	if in.Preview {
		return showPreview(ctx, deps, in)
	}
	if in.Ref.IsZero() {
		return api.Show{}, &NotFoundError{Ref: in.Ref}
	}
	model, err := seriesfile.Load(deps.LibRoot, in.Ref)
	if err != nil {
		// Surface a typed not-found rather than leaking the raw
		// os.ErrNotExist with its on-disk path; transports map this
		// to a 404 with a clean "series not tracked" message.
		if errors.Is(err, os.ErrNotExist) {
			return api.Show{}, &SeriesNotFoundError{Ref: in.Ref}
		}
		return api.Show{}, err
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
	rowOpts := rowBuildOptions(deps)
	filter, noEpisodes := showEpisodeFilter(model, now, in.Episodes, in.Status, in.Source, in.Resolution, rowOpts)
	selector := filter.selector
	if selector.IsNormal() {
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
			return api.Show{}, &EpisodeSelectorSeasonMissingError{
				Ref:      in.Ref,
				Selector: selector.String(),
				Season:   selector.Season,
			}
		}
	}
	row := indexfile.BuildRowFromModelWithOptions(model, now, rowOpts)
	seasons := []api.SeasonShow{}
	if !noEpisodes {
		seasons = buildSeasons(seriesRoot, model, now, filter, false)
	}
	out := api.Show{
		MetadataRef:    model.Metadata,
		Ref:            in.Ref,
		Root:           librarySelector(deps.LibRoot, seriesRoot),
		LastScanned:    formatOptionalTime(model.LastScanned),
		PreferredTitle: preferredTitle,
		CanonicalTitle: model.CanonicalTitle.String(),
		Tags:           slices.Clone(model.Tags),
		Status:         row.Status,
		IsAiring:       row.IsAiring,
		Seasons:        seasons,
		StagedTrash:    buildStagedTrash(seriesRoot, model.StagedTrash),
		StagedExtras:   buildStagedExtras(deps.InboxRoot, model.StagedExtras),
		Artwork:        artworkShow(model.Artwork),
	}
	return out, nil
}

// showPreview builds a Show from live provider metadata for a series
// that isn't in the library yet (MetadataRef, not a local series.json).
// The episode spine comes from the provider; every episode reports as
// missing since nothing is on disk. Backs the UI's pre-add preview
// (GET /series/{ref}?preview=true).
func showPreview(ctx context.Context, deps Deps, in ShowInput) (api.Show, error) {
	metadataSeries, metadataRef, err := fetchSeriesMetadata(ctx, deps, in.MetadataRef, "")
	if err != nil {
		return api.Show{}, err
	}
	// Derive the directory name the series would get on add, so the
	// preview shows its eventual Ref.
	ref, err := resolveAddRef(refs.Series{}, metadataSeries)
	if err != nil {
		return api.Show{}, err
	}
	model, err := seriesfile.NewFromMetadata(metadataRef, "", metadataSeries)
	if err != nil {
		return api.Show{}, err
	}
	model.Ref = ref
	now := in.Now
	if now.IsZero() {
		now = deps.Now()
	}
	seriesRoot := paths.SeriesDir(deps.LibRoot, ref)
	rowOpts := rowBuildOptions(deps)
	filter, noEpisodes := showEpisodeFilter(model, now, in.Episodes, in.Status, in.Source, in.Resolution, rowOpts)
	row := indexfile.BuildRowFromModelWithOptions(model, now, rowOpts)
	preferredTitle := model.PreferredTitle.String()
	if preferredTitle == "" {
		preferredTitle = ref.String()
	}
	seasons := []api.SeasonShow{}
	if !noEpisodes {
		seasons = buildSeasons(seriesRoot, model, now, filter, true)
	}
	return api.Show{
		MetadataRef:    metadataRef,
		Ref:            ref,
		Root:           librarySelector(deps.LibRoot, seriesRoot),
		PreferredTitle: preferredTitle,
		CanonicalTitle: model.CanonicalTitle.String(),
		Status:         row.Status,
		IsAiring:       row.IsAiring,
		Seasons:        seasons,
		Artwork:        artworkShow(model.Artwork),
	}, nil
}

// artworkShow maps persisted series artwork to its response shape.
// Returns nil when there's no artwork so the field omits cleanly.
func artworkShow(a domainseries.Artwork) *api.ArtworkShow {
	if a.IsZero() {
		return nil
	}
	out := &api.ArtworkShow{}
	if !a.Poster.IsZero() {
		out.Poster = &api.PosterShow{
			URL:          a.Poster.URL,
			ThumbnailURL: a.Poster.ThumbnailURL,
			Language:     a.Poster.Language,
		}
	}
	return out
}

func buildStagedTrash(seriesRoot string, items []domainseries.StagedTrashItem) []api.TrashItemShow {
	if len(items) == 0 {
		return nil
	}
	sorted := make([]domainseries.StagedTrashItem, len(items))
	copy(sorted, items)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID.Compare(sorted[j].ID) < 0 })
	out := make([]api.TrashItemShow, 0, len(sorted))
	for _, item := range sorted {
		companions := make([]api.CompanionShow, 0, len(item.Companions))
		for _, c := range item.Companions {
			companions = append(companions, api.CompanionShow{
				Path:     seriesSelector(seriesRoot, c.Path),
				Role:     c.Role,
				Language: c.Language,
				Label:    c.Label,
				Size:     c.Size,
				MTime:    c.MTime.UTC().Format(time.RFC3339),
			})
		}
		out = append(out, api.TrashItemShow{
			ID:         item.ID.String(),
			Path:       seriesSelector(seriesRoot, item.Path),
			Size:       item.Size,
			MTime:      item.MTime.UTC().Format(time.RFC3339),
			AddedAt:    formatOptionalTime(item.AddedAt),
			Companions: companions,
		})
	}
	return out
}

func buildStagedExtras(inboxRoot string, items []domainseries.StagedExtraItem) []api.ExtraItemShow {
	if len(items) == 0 {
		return nil
	}
	sorted := make([]domainseries.StagedExtraItem, len(items))
	copy(sorted, items)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID.Compare(sorted[j].ID) < 0 })
	out := make([]api.ExtraItemShow, 0, len(sorted))
	for _, item := range sorted {
		out = append(out, api.ExtraItemShow{
			ID:      item.ID.String(),
			Season:  item.Season,
			Path:    inboxSelector(inboxRoot, item.Path),
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
	selector           refs.EpisodeSelector
	seasonFilterActive bool
	seasons            map[int]struct{}
	statuses           map[api.Status]struct{}
	sources            map[string]struct{}
	resolutions        map[string]struct{}
}

func (f episodeFilter) match(ref refs.Episode, view api.EpisodeShow) bool {
	if f.seasonFilterActive {
		if _, ok := f.seasons[ref.Season()]; !ok {
			return false
		}
	}
	if f.selector.IsNormal() && !f.selector.Matches(ref) {
		return false
	}
	if len(f.statuses) > 0 {
		if _, ok := f.statuses[view.Status]; !ok && !matchesCollapsedStatus(f.statuses, view.Status) {
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

func showEpisodeFilter(
	model *domainseries.Series,
	now time.Time,
	selector refs.EpisodeSelector,
	statuses []api.Status,
	sources []string,
	resolutions []string,
	rowOpts indexfile.BuildOptions,
) (episodeFilter, bool) {
	filter := episodeFilter{
		statuses:    statusSet(statuses),
		sources:     stringSet(sources),
		resolutions: stringSet(resolutions),
	}
	switch {
	case selector.IsNone():
		return filter, true
	case selector.IsAiringSeason():
		filter.seasonFilterActive = true
		filter.seasons = indexfile.AiringSeasons(model, now, rowOpts)
	case selector.IsNormal():
		filter.selector = selector
	}
	return filter, false
}

func matchesCollapsedStatus(statuses map[api.Status]struct{}, status api.Status) bool {
	if status != api.StatusStagedReplacement {
		return false
	}
	_, ok := statuses[api.StatusStaged]
	return ok
}

func statusSet(in []api.Status) map[api.Status]struct{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[api.Status]struct{}, len(in))
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

func buildSeasons(seriesRoot string, model *domainseries.Series, now time.Time, filter episodeFilter, forceMissing bool) []api.SeasonShow {
	bySeason := map[int][]api.EpisodeShow{}
	allSeasons := map[int]struct{}{}
	for ref, episode := range model.Episodes {
		allSeasons[ref.Season()] = struct{}{}
		status := computeEpisodeStatus(episode, now)
		if forceMissing {
			// Preview: nothing is on disk, so every episode reads as
			// missing (collapses pending/future into missing too).
			status = api.StatusMissing
		}
		view := api.EpisodeShow{
			Episode:        ref,
			Aired:          formatAirDate(episode.AirDate),
			Status:         status,
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
	// Surface every season the filter would visit. A concrete selector
	// scopes to one season, AIRING_SEASON scopes to its computed season
	// set, and default/ALL surfaces every season present in the spine
	// even if later axes trimmed it to zero episodes.
	surfaceFilteredSeasons(bySeason, allSeasons, filter)
	numbers := make([]int, 0, len(bySeason))
	for n := range bySeason {
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)
	out := make([]api.SeasonShow, 0, len(numbers))
	for _, n := range numbers {
		eps := bySeason[n]
		sort.Slice(eps, func(i, j int) bool { return eps[i].Episode.Episode() < eps[j].Episode.Episode() })
		out = append(out, api.SeasonShow{
			Number:   n,
			Summary:  summarizeSeason(eps),
			Episodes: eps,
		})
	}
	return out
}

func surfaceFilteredSeasons(bySeason map[int][]api.EpisodeShow, allSeasons map[int]struct{}, filter episodeFilter) {
	switch {
	case filter.selector.IsNormal():
		// Only the selected season; verified to exist by caller.
		if _, ok := bySeason[filter.selector.Season]; !ok {
			bySeason[filter.selector.Season] = nil
		}
	case filter.seasonFilterActive:
		for s := range filter.seasons {
			if _, ok := bySeason[s]; !ok {
				bySeason[s] = nil
			}
		}
	default:
		for s := range allSeasons {
			if _, ok := bySeason[s]; !ok {
				bySeason[s] = nil
			}
		}
	}
}

func summarizeSeason(eps []api.EpisodeShow) api.SeasonSummary {
	out := api.SeasonSummary{EpisodeCount: len(eps)}
	for _, ep := range eps {
		switch ep.Status {
		case api.StatusPresent:
			out.Present++
		case api.StatusMissing:
			out.Missing++
		case api.StatusStaged:
			out.Staged++
		case api.StatusStagedReplacement:
			out.StagedReplacement++
		case api.StatusPending:
			out.Pending++
		}
	}
	return out
}

func computeEpisodeStatus(episode domainseries.Episode, now time.Time) api.Status {
	if episode.Active != nil && episode.Staged != nil {
		return api.StatusStagedReplacement
	}
	if episode.Staged != nil {
		return api.StatusStaged
	}
	if episode.Active != nil {
		return api.StatusPresent
	}
	if isPending(episode.AirDate, now) {
		return api.StatusPending
	}
	return api.StatusMissing
}

func mediaShow(seriesRoot string, record media.Record) api.MediaShow {
	companions := make([]api.CompanionShow, 0, len(record.Companions))
	for _, c := range record.Companions {
		companions = append(companions, api.CompanionShow{
			Path:     seriesSelector(seriesRoot, c.Path),
			Role:     c.Role,
			Language: c.Language,
			Label:    c.Label,
			Size:     c.Size,
			MTime:    c.MTime.UTC().Format(time.RFC3339),
		})
	}
	dimensions := ""
	if record.Resolution.Known() {
		dimensions = record.Resolution.String()
	}
	return api.MediaShow{
		Source:     record.Source.Display(),
		Resolution: record.Resolution.Display(),
		Dimensions: dimensions,
		Codec:      record.Codec.String(),
		Size:       record.Size,
		MTime:      formatOptionalTime(record.MTime),
		File:       seriesSelector(seriesRoot, record.Path),
		Companions: companions,
		Attrs:      media.CloneAttrs(record.Attrs),
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
		return true
	}
	return aired.After(civil.DateOf(now))
}

func formatAirDate(value civil.Date) string {
	if !value.IsValid() {
		return ""
	}
	return value.String()
}
