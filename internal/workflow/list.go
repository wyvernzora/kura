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

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
)

// ListInput parameters for the List workflow. An empty Statuses slice
// returns every row; a non-empty slice filters in.
type ListInput struct {
	Statuses []response.ListStatus
	Now      time.Time
}

// List walks the library root, builds one row per series subdirectory
// via indexfile.BuildRow (single source of truth for row shape),
// optionally filters by status, and returns a sorted ListResult.
//
// Phase 2 still walks the disk on every call. Phase 5 swaps this for
// an in-memory read against deps.Index.
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

// buildListRow turns one library subdirectory name into a response row.
// Parse failures on the directory name surface as ListStatusError; all
// other shape decisions live in indexfile.BuildRow.
func buildListRow(libRoot string, name string, now time.Time) response.ListRow {
	ref, err := refs.ParseSeries(name)
	if err != nil {
		return response.ListRow{
			Title:  name,
			Status: response.ListStatusError,
			Error:  err.Error(),
		}
	}
	row, err := indexfile.BuildRow(libRoot, ref, now)
	if err != nil {
		return response.ListRow{
			Title:  name,
			Status: response.ListStatusError,
			Error:  err.Error(),
		}
	}
	return rowToListRow(row)
}

func rowToListRow(row indexfile.Row) response.ListRow {
	return response.ListRow{
		Status:            row.Status,
		Staged:            row.Staged,
		Title:             row.Title,
		CanonicalTitle:    row.CanonicalTitle,
		SeasonsAvailable:  row.SeasonsAvailable,
		SeasonCount:       row.SeasonCount,
		EpisodesAvailable: row.EpisodesAvailable,
		EpisodeCount:      row.EpisodeCount,
		MetadataRef:       row.Metadata,
		Resolutions:       row.Resolutions,
		Sources:           row.Sources,
		LastScanned:       row.LastScanned,
		Error:             row.Error,
	}
}

func listStatusAllowed(status response.ListStatus, allowed []response.ListStatus) bool {
	if len(allowed) == 0 {
		return true
	}
	return slices.Contains(allowed, status)
}
