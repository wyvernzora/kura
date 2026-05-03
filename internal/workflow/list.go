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

	progress.Start(ctx, "list", "Listing library contents", 0)
	dir, err := os.Open(deps.LibRoot)
	if err != nil {
		progress.Failure(ctx, "list", "Failed to list library contents", 0, 0)
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
			progress.Failure(ctx, "list", "Failed to list library contents", scanned, 0)
			return response.ListResult{}, readErr
		}
		for _, entry := range entries {
			name := entry.Name()
			if !entry.IsDir() || strings.HasPrefix(name, ".") {
				continue
			}
			scanned++
			progress.Update(ctx, "list", fmt.Sprintf("Listing %s", name), scanned, 0)
			row := buildListRow(deps.LibRoot, name, now)
			if listStatusAllowed(row.Status, in.Statuses) {
				rows = append(rows, row)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Root < rows[j].Root })
	progress.Success(ctx, "list", fmt.Sprintf("Listed library contents (%d series)", len(rows)), scanned)
	return response.ListResult{Rows: rows}, nil
}

func buildListRow(libRoot string, name string, now time.Time) response.ListRow {
	row := response.ListRow{
		Title: name,
		Root:  name,
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
	row.SeasonCount = summary.seasons
	row.EpisodeCount = summary.episodes
	row.MetadataRef = model.Metadata
	row.LastScanned = formatOptionalTime(model.LastScanned)
	row.Staged = summary.hasStaged
	row.Status = listStatusFor(summary)
	return row
}

type seriesSummary struct {
	seasons   int
	episodes  int
	missing   int
	pending   int
	hasStaged bool
}

func summarizeSeries(model *domainseries.Series, now time.Time) seriesSummary {
	var s seriesSummary
	seasons := map[int]struct{}{}
	for episodeRef, episode := range model.Episodes {
		if episode.Staged != nil {
			s.hasStaged = true
		}
		if episodeRef.IsSpecial() {
			continue
		}
		s.episodes++
		seasons[episodeRef.Season()] = struct{}{}
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
