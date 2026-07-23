package indexfile

import (
	"context"
	"errors"
	"os"
	"slices"
	"sort"
	"time"

	"cloud.google.com/go/civil"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/series"
	"github.com/wyvernzora/kura/services/library-manager/internal/response"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/paths"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/seriesfile"
)

const (
	// airingHorizonDays is the window a cour's first episode may sit
	// in the future and still count as "airing now". One week.
	airingHorizonDays = 7

	// defaultAiringTailDays is the grace window after a cour's last
	// episode airs where it still counts as airing for maintenance.
	defaultAiringTailDays = 7

	// airingCourGapDays is the minimum gap between two consecutive
	// air dates that splits a season into separate cours. Anime
	// weekly cadence is 7d; a one-week skip is 14d; this leaves
	// 30d as a safe split threshold that catches split-cour /
	// production-break gaps without false-splitting normal cadence.
	airingCourGapDays = 30
)

// DefaultBuildOptions returns Kura's row-building policy defaults.
func DefaultBuildOptions() BuildOptions {
	return BuildOptions{
		AiringTailDays: defaultAiringTailDays,
	}
}

// diskEntryBuilder loads <libRoot>/<ref>/.kura/series.json and returns a source
// entry describing the series. Behaviors:
//
//   - series.json present and parseable: tracked model entry.
//   - .kura/ directory absent: untracked entry.
//     The directory has never been a tracked series.
//   - .kura/ directory present but series.json missing: error entry. Genuine
//     "lost the file" condition (transient I/O on a network volume, partial
//     crash mid-write, hand deletion).
//     Surfacing as untracked here would silently drop the metadataRef
//     from the index, hiding the corruption.
//   - series.json present but unreadable / malformed: error entry. The error is
//     captured in the entry, not returned, so a single broken series doesn't
//     fail the whole walk.
func diskEntryBuilder(_ context.Context, libRoot string, ref refs.Series) (Entry, error) {
	if ref.IsZero() {
		return Entry{}, errors.New("indexfile: disk entry builder requires non-zero ref")
	}
	exists, err := seriesfile.Exists(libRoot, ref)
	if err != nil {
		return Entry{Series: ref, Error: err.Error()}, nil
	}
	if !exists {
		kuraDir := paths.SeriesKuraDir(libRoot, ref)
		info, statErr := os.Stat(kuraDir)
		if statErr == nil && info.IsDir() {
			return Entry{Series: ref, Error: "series.json missing from .kura/ — file disappeared under index walk"}, nil
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return Entry{Series: ref, Error: statErr.Error()}, nil
		}
		return Entry{Series: ref}, nil
	}
	model, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		return Entry{Series: ref, Error: err.Error()}, nil
	}
	model.Ref = ref
	return Entry{Model: model}, nil
}

// BuildRowFromModel computes a Row from an already-loaded *series.Series.
// Pure function; no I/O. Used by the Index's query-time projections and by
// Show to build response rollups from a freshly loaded model.
func BuildRowFromModel(model *series.Series, now time.Time) Row {
	return BuildRowFromModelWithOptions(model, now, DefaultBuildOptions())
}

// BuildRowFromModelWithOptions computes a Row from model using explicit
// row-building policy.
func BuildRowFromModelWithOptions(model *series.Series, now time.Time, opts BuildOptions) Row {
	row := Row{
		Series:      model.Ref,
		Metadata:    model.Metadata,
		Title:       model.Ref.String(),
		DateAdded:   formatOptionalTime(model.DateAdded),
		LastScanned: formatOptionalTime(model.LastScanned),
	}
	if !model.PreferredTitle.IsZero() {
		row.Title = model.PreferredTitle.String()
	}
	row.CanonicalTitle = model.CanonicalTitle.String()

	summary := summarizeSeries(model, now, opts)
	row.SeasonsAvailable = summary.seasonsActive
	row.SeasonCount = summary.seasons
	row.EpisodesAvailable = summary.episodesActive
	row.EpisodeCount = summary.episodes
	row.Staged = summary.hasStaged
	row.Status = listStatusFor(summary)
	row.IsAiring = summary.airing
	if summary.lastAired.IsValid() {
		row.LastAired = summary.lastAired.String()
	}
	row.Resolutions, row.Sources = collectActiveQuality(model)
	row.Tags = slices.Clone(model.Tags)
	if !model.Artwork.Poster.IsZero() {
		row.PosterURL = model.Artwork.Poster.URL
		row.PosterThumbnailURL = model.Artwork.Poster.ThumbnailURL
	}
	row.SearchKey = model.SearchKey
	return row
}

// UntrackedRow synthesizes a Row for a directory that has no series.json.
// Used by Rebuild when walking libRoot dirents.
func UntrackedRow(ref refs.Series, now time.Time) Row {
	return Row{
		Series: ref,
		Title:  ref.String(),
		Status: response.ListStatusUntracked,
	}
}

// seriesSummary mirrors the rollup the workflow.list package used to
// compute. Specials (season 0) are excluded from every counter and from
// hasStaged — they don't factor into series observed state.
type seriesSummary struct {
	seasons        int
	seasonsActive  int
	episodes       int
	episodesActive int
	missing        int
	pending        int
	hasStaged      bool
	airing         bool
	lastAired      civil.Date
}

// seasonAirDates holds the valid AirDates for one non-special season.
// Used by computeAiring to detect cour structure.
type seasonAirDates struct {
	dates []civil.Date
}

func summarizeSeries(model *series.Series, now time.Time, opts BuildOptions) seriesSummary {
	var s seriesSummary
	today := civil.DateOf(now)
	seasons := map[int]struct{}{}
	seasonsActive := map[int]struct{}{}
	for episodeRef, episode := range model.Episodes {
		if episodeRef.IsSpecial() {
			continue
		}
		sn := episodeRef.Season()

		if episode.AirDate.IsValid() {
			if !episode.AirDate.After(today) && (!s.lastAired.IsValid() || s.lastAired.Before(episode.AirDate)) {
				s.lastAired = episode.AirDate
			}
		}

		if episode.Staged != nil {
			s.hasStaged = true
		}

		// Pending = no record AND air date is in the future. Such
		// slots are announced but not yet expected to have a file;
		// they don't count toward EpisodeCount / SeasonCount, so the
		// "X / Y" rollup reflects aired-and-trackable episodes only.
		// Pre-staged pending episodes (Staged != nil) DO count
		// because the file is already in hand.
		if episode.Active == nil && episode.Staged == nil && isPending(episode.AirDate, now) {
			s.pending++
			continue
		}

		s.episodes++
		seasons[sn] = struct{}{}
		if episode.Active != nil {
			s.episodesActive++
			seasonsActive[sn] = struct{}{}
			continue
		}
		if episode.Staged != nil {
			continue
		}
		s.missing++
	}
	s.seasons = len(seasons)
	s.seasonsActive = len(seasonsActive)
	s.airing = len(AiringSeasons(model, now, opts)) > 0
	return s
}

// AiringSeasons returns non-special seasons whose current cour is
// inside Kura's airing window. The tail is intentional: recently ended
// cours stay visible long enough for release-group batches or
// preferred encodes to replace temporary stand-ins.
func AiringSeasons(model *series.Series, now time.Time, opts BuildOptions) map[int]struct{} {
	perSeason := map[int]*seasonAirDates{}
	for episodeRef, episode := range model.Episodes {
		if episodeRef.IsSpecial() || !episode.AirDate.IsValid() {
			continue
		}
		sn := episodeRef.Season()
		sa, ok := perSeason[sn]
		if !ok {
			sa = &seasonAirDates{}
			perSeason[sn] = sa
		}
		sa.dates = append(sa.dates, episode.AirDate)
	}
	return airingSeasonsFromAirDates(perSeason, now, opts.AiringTailDays)
}

// airingSeasonsFromAirDates returns seasons with at least one cour
// that is "currently airing." A season is split into cours by sorting
// its episode air dates and starting a new cour wherever the gap
// between consecutive dates exceeds airingCourGapDays. A cour
// qualifies when (a) its first air date has already passed or falls
// within airingHorizonDays AND (b) its last date is no older than the
// configured airing tail. Far-ahead schedule announcements (first ep
// beyond the horizon) and split-cour hiatuses (the active cour ended
// before the tail; the next cour's first ep is months out) are both
// filtered.
func airingSeasonsFromAirDates(perSeason map[int]*seasonAirDates, now time.Time, tailDays int) map[int]struct{} {
	out := map[int]struct{}{}
	today := civil.DateOf(now)
	horizon := today.AddDays(airingHorizonDays)
	tailStart := today.AddDays(-tailDays)
	for season, sa := range perSeason {
		if len(sa.dates) == 0 {
			continue
		}
		sorted := append([]civil.Date(nil), sa.dates...)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Before(sorted[j])
		})
		for _, cour := range splitIntoCours(sorted, airingCourGapDays) {
			first := cour[0]
			last := cour[len(cour)-1]
			if first.After(horizon) {
				continue
			}
			if !last.Before(tailStart) {
				out[season] = struct{}{}
				break
			}
		}
	}
	return out
}

// splitIntoCours partitions a sorted-ascending slice of air dates into
// contiguous cours. A new cour begins wherever the gap to the previous
// date exceeds gapDays. Empty input returns nil.
func splitIntoCours(sorted []civil.Date, gapDays int) [][]civil.Date {
	if len(sorted) == 0 {
		return nil
	}
	out := [][]civil.Date{{sorted[0]}}
	for i := 1; i < len(sorted); i++ {
		if sorted[i].DaysSince(sorted[i-1]) > gapDays {
			out = append(out, []civil.Date{sorted[i]})
			continue
		}
		out[len(out)-1] = append(out[len(out)-1], sorted[i])
	}
	return out
}

func listStatusFor(summary seriesSummary) response.ListStatus {
	if summary.episodes == 0 && summary.pending == 0 {
		return response.ListStatusIncomplete
	}
	if summary.missing > 0 {
		return response.ListStatusIncomplete
	}
	return response.ListStatusComplete
}

// collectActiveQuality walks active records on non-special episodes and
// returns the distinct resolutions and sources, sorted high-quality-first
// via media.Source.Rank / by pixel count for resolutions.
func collectActiveQuality(model *series.Series) (resolutions, sources []string) {
	resSeen := map[string]int{}
	srcSeen := map[string]int{}
	for episodeRef, episode := range model.Episodes {
		if episodeRef.IsSpecial() {
			continue
		}
		if episode.Active == nil {
			continue
		}
		if r := episode.Active.Resolution.Display(); r != "" {
			resSeen[r] = episode.Active.Resolution.Width() * episode.Active.Resolution.Height()
		}
		if s := episode.Active.Source.Display(); s != "" {
			srcSeen[s] = episode.Active.Source.Rank()
		}
	}
	resolutions = sortByValueDesc(resSeen)
	sources = sortByValueDesc(srcSeen)
	return resolutions, sources
}

func sortByValueDesc(m map[string]int) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if m[out[i]] != m[out[j]] {
			return m[out[i]] > m[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}

// isPending reports whether an episode slot is announced but not yet
// expected to have media. Future air dates and TBA placeholders (no
// valid AirDate) both qualify — the latter is the strongest form of
// "not aired yet" and must not count as missing media.
func isPending(aired civil.Date, now time.Time) bool {
	if !aired.IsValid() {
		return true
	}
	return aired.After(civil.DateOf(now))
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
