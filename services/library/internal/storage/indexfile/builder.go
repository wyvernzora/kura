package indexfile

import (
	"errors"
	"sort"
	"time"

	"cloud.google.com/go/civil"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

// RowBuilder produces a fully-populated Row for a given series ref. The
// builder owns the policy for what a row looks like — counts, status,
// quality rollups — and is shared between Rebuild (full disk walk),
// the watcher's rebuild loop, and synchronous-mutation row updates.
type RowBuilder func(libRoot string, ref refs.Series, now time.Time) (Row, error)

// BuildRow loads <libRoot>/<ref>/.kura/series.json and returns a Row
// describing the series. Behaviors:
//
//   - series.json present and parseable: Row reflects the model
//     (counts, quality rollups, lastScanned).
//   - series.json absent: Row{Status: untracked, Title: ref}.
//   - series.json present but unreadable / malformed: Row{Status: error,
//     Error: msg, Title: ref}. The error is captured in the row, not
//     returned, so a single broken series doesn't fail the whole walk.
//
// UpdatedAt is stamped to now in every case.
func BuildRow(libRoot string, ref refs.Series, now time.Time) (Row, error) {
	if ref.IsZero() {
		return Row{}, errors.New("indexfile: BuildRow requires non-zero ref")
	}
	exists, err := seriesfile.Exists(libRoot, ref)
	if err != nil {
		return Row{
			Series:    ref,
			Title:     ref.String(),
			Status:    response.ListStatusError,
			Error:     err.Error(),
			UpdatedAt: now.UTC().Format(time.RFC3339),
		}, nil
	}
	if !exists {
		return Row{
			Series:    ref,
			Title:     ref.String(),
			Status:    response.ListStatusUntracked,
			UpdatedAt: now.UTC().Format(time.RFC3339),
		}, nil
	}
	model, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		return Row{
			Series:    ref,
			Title:     ref.String(),
			Status:    response.ListStatusError,
			Error:     err.Error(),
			UpdatedAt: now.UTC().Format(time.RFC3339),
		}, nil
	}
	model.Ref = ref
	return BuildRowFromModel(model, now), nil
}

// BuildRowFromModel computes a Row from an already-loaded *series.Series.
// Pure function; no I/O. Used by mutators after a successful series.json
// SaveCAS to refresh the index without re-reading the file.
func BuildRowFromModel(model *series.Series, now time.Time) Row {
	row := Row{
		Series:      model.Ref,
		Metadata:    model.Metadata,
		Title:       model.Ref.String(),
		LastScanned: formatOptionalTime(model.LastScanned),
		UpdatedAt:   now.UTC().Format(time.RFC3339),
	}
	if !model.PreferredTitle.IsZero() {
		row.Title = model.PreferredTitle.String()
	}
	row.CanonicalTitle = model.CanonicalTitle.String()

	summary := summarizeSeries(model, now)
	row.SeasonsAvailable = summary.seasonsActive
	row.SeasonCount = summary.seasons
	row.EpisodesAvailable = summary.episodesActive
	row.EpisodeCount = summary.episodes
	row.Staged = summary.hasStaged
	row.Status = listStatusFor(summary)
	row.Resolutions, row.Sources = collectActiveQuality(model)
	return row
}

// UntrackedRow synthesizes a Row for a directory that has no series.json.
// Used by Rebuild when walking libRoot dirents.
func UntrackedRow(ref refs.Series, now time.Time) Row {
	return Row{
		Series:    ref,
		Title:     ref.String(),
		Status:    response.ListStatusUntracked,
		UpdatedAt: now.UTC().Format(time.RFC3339),
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
}

func summarizeSeries(model *series.Series, now time.Time) seriesSummary {
	var s seriesSummary
	seasons := map[int]struct{}{}
	seasonsActive := map[int]struct{}{}
	for episodeRef, episode := range model.Episodes {
		if episodeRef.IsSpecial() {
			continue
		}
		if episode.Staged != nil {
			s.hasStaged = true
		}
		s.episodes++
		seasons[episodeRef.Season()] = struct{}{}
		if episode.Active != nil {
			s.episodesActive++
			seasonsActive[episodeRef.Season()] = struct{}{}
		}
		if episode.Active != nil || episode.Staged != nil {
			continue
		}
		if isPending(episode.AirDate, now) {
			s.pending++
			continue
		}
		s.missing++
	}
	s.seasons = len(seasons)
	s.seasonsActive = len(seasonsActive)
	return s
}

func listStatusFor(summary seriesSummary) response.ListStatus {
	if summary.episodes == 0 {
		return response.ListStatusIncomplete
	}
	if summary.missing > 0 {
		return response.ListStatusIncomplete
	}
	if summary.pending > 0 {
		return response.ListStatusAiring
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

func isPending(aired civil.Date, now time.Time) bool {
	if !aired.IsValid() {
		return false
	}
	return aired.After(civil.DateOf(now))
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
