package workflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/civil"
	"github.com/wyvernzora/kura/internal/domain/refs"
	domainseries "github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

// ListInput parameters for the List workflow. An empty Statuses slice
// returns every row; a non-empty slice filters in.
type ListInput struct {
	Statuses []response.ListStatus
	Now      time.Time
}

// List walks the library root, builds one row per series subdirectory,
// optionally filters by status, and returns a sorted ListResult.
func List(ctx context.Context, deps Deps, in ListInput) (response.ListResult, error) {
	info, err := os.Stat(deps.LibRoot)
	if errors.Is(err, os.ErrNotExist) {
		return response.ListResult{}, ErrLibraryRootNotFound
	}
	if err != nil {
		return response.ListResult{}, err
	}
	if !info.IsDir() {
		return response.ListResult{}, ErrLibraryRootNotDirectory
	}

	// Approximate the walk total from the in-memory index: counts
	// tracked series under the library root, which is a strict subset
	// of the directories the walk will visit (untracked dirs are
	// missing). Close enough for a progress hint; the operator sees
	// the count overshoot by the untracked count, never undershoot
	// past the total. Falls back to TotalIndeterminate when the
	// index is empty (fresh library, pre-reindex).
	estimatedTotal := deps.Index.Len()
	if estimatedTotal == 0 {
		estimatedTotal = progress.TotalIndeterminate
	}
	progress.Start(ctx, "list", "Listing library contents", estimatedTotal)
	dir, err := os.Open(deps.LibRoot)
	if err != nil {
		progress.Failure(ctx, "list", "Failed to list library contents", 0, estimatedTotal)
		return response.ListResult{}, err
	}
	defer dir.Close()

	now := in.Now
	if now.IsZero() {
		now = deps.Now()
	}

	var rows []response.ListRow
	scanned := 0
	for {
		entries, readErr := dir.ReadDir(64)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			progress.Failure(ctx, "list", "Failed to list library contents", scanned, estimatedTotal)
			return response.ListResult{}, readErr
		}
		for _, entry := range entries {
			name := entry.Name()
			if !entry.IsDir() || strings.HasPrefix(name, ".") {
				continue
			}
			scanned++
			progress.Update(ctx, "list", fmt.Sprintf("Listing %s", name), scanned, estimatedTotal)
			row := buildListRow(deps.LibRoot, name, now)
			if listStatusAllowed(row.Status, in.Statuses) {
				rows = append(rows, row)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Title < rows[j].Title })
	progress.Success(ctx, "list", fmt.Sprintf("Listed library contents (%d series)", len(rows)), scanned)
	return response.ListResult{Rows: rows}, nil
}

func buildListRow(libRoot string, name string, now time.Time) response.ListRow {
	row := response.ListRow{
		Title: name,
	}
	ref, err := refs.ParseSeries(name)
	if err != nil {
		row.Status = response.ListStatusError
		row.Error = err.Error()
		return row
	}
	exists, err := seriesfile.Exists(libRoot, ref)
	if err != nil {
		row.Status = response.ListStatusError
		row.Error = err.Error()
		return row
	}
	if !exists {
		row.Status = response.ListStatusUntracked
		return row
	}
	model, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		row.Status = response.ListStatusError
		row.Error = err.Error()
		return row
	}
	summary := summarizeSeries(model, now)
	if !model.PreferredTitle.IsZero() {
		row.Title = model.PreferredTitle.String()
	}
	row.CanonicalTitle = model.CanonicalTitle.String()
	row.SeasonsAvailable = summary.seasonsActive
	row.SeasonCount = summary.seasons
	row.EpisodesAvailable = summary.episodesActive
	row.EpisodeCount = summary.episodes
	row.MetadataRef = model.Metadata
	row.LastScanned = formatOptionalTime(model.LastScanned)
	row.Staged = summary.hasStaged
	row.Status = listStatusFor(summary)
	row.Resolutions, row.Sources = collectActiveQuality(model)
	return row
}

// collectActiveQuality walks active records on non-special episodes
// and returns the distinct resolutions and sources, sorted high-
// quality-first via media.Source.Rank / by pixel count for resolutions.
func collectActiveQuality(model *domainseries.Series) (resolutions, sources []string) {
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

// sortByValueDesc returns the keys of m sorted by their integer
// values in descending order, with ties broken alphabetically for
// determinism.
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

type seriesSummary struct {
	seasons        int
	seasonsActive  int
	episodes       int
	episodesActive int
	missing        int
	pending        int
	hasStaged      bool
}

// summarizeSeries derives the row's observed-state inputs from a
// loaded series model. Specials (season 0) are excluded from every
// counter and from hasStaged — they do not factor into series
// observed state per Product.md.
func summarizeSeries(model *domainseries.Series, now time.Time) seriesSummary {
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

func listStatusAllowed(status response.ListStatus, allowed []response.ListStatus) bool {
	if len(allowed) == 0 {
		return true
	}
	return slices.Contains(allowed, status)
}

func isPending(aired civil.Date, now time.Time) bool {
	if !aired.IsValid() {
		return false
	}
	return aired.After(civil.DateOf(now))
}
