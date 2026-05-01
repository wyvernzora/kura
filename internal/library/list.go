package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/series"
)

type ListStatus string

const (
	ListStatusUntracked  ListStatus = "untracked"
	ListStatusComplete   ListStatus = "complete"
	ListStatusIncomplete ListStatus = "incomplete"
	ListStatusAiring     ListStatus = "airing"
	ListStatusError      ListStatus = "error"
)

type ListEntry struct {
	Status         ListStatus    `json:"status"`
	Staged         bool          `json:"staged,omitempty"`
	Title          string        `json:"title"`
	CanonicalTitle string        `json:"canonicalTitle,omitempty"`
	SeasonCount    int           `json:"seasonCount"`
	EpisodeCount   int           `json:"episodeCount"`
	Root           string        `json:"root"`
	MetadataRef    refs.Metadata `json:"metadataRef,omitempty"`
	Error          string        `json:"error,omitempty"`
}

type ListInput struct {
	Root string
	Now  time.Time
}

func List(ctx context.Context, in ListInput) ([]ListEntry, error) {
	info, err := os.Stat(in.Root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrRootNotFound
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, ErrRootNotDirectory
	}
	root, err := ParseRoot(in.Root)
	if err != nil {
		return nil, err
	}
	progress.Start(ctx, "list", "Listing library contents", 0)
	dir, err := os.Open(root.Path())
	if err != nil {
		progress.Failure(ctx, "list", "Failed to list library contents", 0, 0)
		return nil, err
	}
	defer dir.Close()

	var entries []ListEntry
	scanned := 0
	for {
		dirEntries, err := dir.ReadDir(64)
		if err != nil && !errors.Is(err, io.EOF) {
			progress.Failure(ctx, "list", "Failed to list library contents", scanned, 0)
			return nil, err
		}
		for _, dirEntry := range dirEntries {
			name := dirEntry.Name()
			if !dirEntry.IsDir() || strings.HasPrefix(name, ".") {
				continue
			}
			scanned++
			progress.Update(ctx, "list", fmt.Sprintf("Listing %s", name), scanned, 0)
			entries = append(entries, listOne(root, name, in.Now))
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Root < entries[j].Root
	})
	progress.Success(ctx, "list", fmt.Sprintf("Listed library contents (%d series)", len(entries)), scanned)
	return entries, nil
}

func listOne(root Root, name string, now time.Time) ListEntry {
	entry := ListEntry{
		Title: name,
		Root:  name,
	}
	ref, err := refs.ParseSeries(name)
	if err != nil {
		entry.Status = ListStatusError
		entry.Error = err.Error()
		return entry
	}
	summary, err := series.ReadListSummary(root.Path(), ref, now)
	if errors.Is(err, os.ErrNotExist) {
		entry.Status = ListStatusUntracked
		entry.Title = name + "*"
		return entry
	}
	if err != nil {
		entry.Status = ListStatusError
		entry.Error = err.Error()
		return entry
	}
	entry.Title = summary.PreferredTitle.String()
	entry.CanonicalTitle = summary.CanonicalTitle.String()
	entry.SeasonCount = summary.SeasonCount
	entry.EpisodeCount = summary.EpisodeCount
	entry.MetadataRef = summary.MetadataRef
	entry.Staged = summary.HasStaged
	entry.Status = listStatus(summary)
	return entry
}

func listStatus(summary series.ListSummary) ListStatus {
	if summary.EpisodeCount == 0 {
		return ListStatusIncomplete
	}
	if summary.MissingCount > 0 {
		return ListStatusIncomplete
	}
	if summary.PendingCount > 0 {
		return ListStatusAiring
	}
	return ListStatusComplete
}
