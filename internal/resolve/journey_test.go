package resolve

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/store"
)

func TestResolverJourneys(t *testing.T) {
	t.Run("clean bootstrap text resolves", func(t *testing.T) {
		source := &strategyFakeSource{
			searchResults: []metadata.SearchResult{{SeriesSummary: testSummary("tvdb:370070")}},
		}
		resolver := New(NewTextSearchStrategy(source))
		resolution, err := resolver.Resolve(context.Background(), ParseQuery([]string{"本好きの下剋上"}))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !resolution.IsResolved() {
			t.Fatalf("IsResolved = false, results = %#v", resolution.Results)
		}
	})

	t.Run("garbled bootstrap unresolved", func(t *testing.T) {
		source := &strategyFakeSource{
			searchResults: []metadata.SearchResult{
				{SeriesSummary: testSummary("tvdb:1")},
				{SeriesSummary: testSummary("tvdb:2")},
				{SeriesSummary: testSummary("tvdb:3")},
			},
		}
		resolver := New(NewTextSearchStrategy(source))
		resolution, err := resolver.Resolve(context.Background(), ParseQuery([]string{"Ascendance.of.a.Bookworm.S01.1080p.WEB-DL"}))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !resolution.IsUnresolved() {
			t.Fatalf("IsUnresolved = false, results = %#v", resolution.Results)
		}
	})

	t.Run("multi-language text terms agree", func(t *testing.T) {
		source := &strategyFakeSource{
			searchResultsByQuery: map[string][]metadata.SearchResult{
				"本好きの下剋上": {
					{SeriesSummary: testSummary("tvdb:370070")},
				},
				"Ascendance of a Bookworm": {
					{SeriesSummary: testSummary("tvdb:370070")},
				},
			},
		}
		resolver := New(NewTextSearchStrategy(source))
		resolution, err := resolver.Resolve(context.Background(), ParseQuery([]string{
			"本好きの下剋上",
			"Ascendance of a Bookworm",
		}))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !resolution.IsResolved() {
			t.Fatalf("IsResolved = false, results = %#v", resolution.Results)
		}
		if got := len(resolution.Results[0].Evidence); got != 2 {
			t.Fatalf("evidence count = %d, want 2", got)
		}
	})

	t.Run("direct id retry resolves", func(t *testing.T) {
		source := &strategyFakeSource{series: map[string]metadata.Series{"370070": testMetadataSeries("tvdb:370070")}}
		resolver := New(NewProviderIDStrategy(source), NewTextSearchStrategy(source))
		resolution, err := resolver.Resolve(context.Background(), ParseQuery([]string{"tvdb:370070"}))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !resolution.IsResolved() {
			t.Fatalf("IsResolved = false, results = %#v", resolution.Results)
		}
	})

	t.Run("dirname term resolves tracked dir", func(t *testing.T) {
		rootDir := t.TempDir()
		writeTrackedSeries(t, filepath.Join(rootDir, "Bookworm"), []string{"tvdb:370070"}, "tvdb")
		root, err := fsroot.ParseLibraryRoot(rootDir)
		if err != nil {
			t.Fatalf("ParseLibraryRoot: %v", err)
		}
		repo := store.NewRepo()
		source := &strategyFakeSource{series: map[string]metadata.Series{"370070": testMetadataSeries("tvdb:370070")}}
		resolver := New(NewDirnameStrategy(root, &repo, source), NewProviderIDStrategy(source), NewTextSearchStrategy(source))

		resolution, err := resolver.Resolve(context.Background(), ParseQuery([]string{"dir:Bookworm"}))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !resolution.IsResolved() {
			t.Fatalf("IsResolved = false, results = %#v", resolution.Results)
		}
	})

	t.Run("unknown query not found", func(t *testing.T) {
		source := &strategyFakeSource{}
		resolver := New(NewTextSearchStrategy(source))
		resolution, err := resolver.Resolve(context.Background(), ParseQuery([]string{"Season 2"}))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !resolution.IsNotFound() {
			t.Fatalf("IsNotFound = false, results = %#v", resolution.Results)
		}
	})

	t.Run("provider down errors", func(t *testing.T) {
		source := &strategyFakeSource{searchErr: metadata.ErrUnavailable}
		resolver := New(NewTextSearchStrategy(source))
		_, err := resolver.Resolve(context.Background(), ParseQuery([]string{"Bookworm"}))
		if !errors.Is(err, metadata.ErrUnavailable) {
			t.Fatalf("error = %v, want ErrUnavailable", err)
		}
	})

	t.Run("stale dirname ref errors", func(t *testing.T) {
		rootDir := t.TempDir()
		writeTrackedSeries(t, filepath.Join(rootDir, "Bookworm"), []string{"tvdb:99999"}, "tvdb")
		root, err := fsroot.ParseLibraryRoot(rootDir)
		if err != nil {
			t.Fatalf("ParseLibraryRoot: %v", err)
		}
		repo := store.NewRepo()
		source := &strategyFakeSource{seriesErr: metadata.ErrNotFound}
		resolver := New(NewDirnameStrategy(root, &repo, source))
		_, err = resolver.Resolve(context.Background(), ParseQuery([]string{"dir:Bookworm"}))
		if !errors.Is(err, ErrStaleProviderRef) {
			t.Fatalf("error = %v, want ErrStaleProviderRef", err)
		}
	})

	t.Run("untracked dirname not found", func(t *testing.T) {
		rootDir := t.TempDir()
		mkdir(t, filepath.Join(rootDir, "Bookworm"))
		root, err := fsroot.ParseLibraryRoot(rootDir)
		if err != nil {
			t.Fatalf("ParseLibraryRoot: %v", err)
		}
		repo := store.NewRepo()
		source := &strategyFakeSource{}
		resolver := New(NewDirnameStrategy(root, &repo, source))
		resolution, err := resolver.Resolve(context.Background(), ParseQuery([]string{"dir:Bookworm"}))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !resolution.IsNotFound() {
			t.Fatalf("IsNotFound = false, results = %#v", resolution.Results)
		}
	})

	t.Run("conflicting terms error", func(t *testing.T) {
		source := &strategyFakeSource{}
		resolver := New(NewProviderIDStrategy(source), NewTextSearchStrategy(source))
		_, err := resolver.Resolve(context.Background(), ParseQuery([]string{"X-Men", "tvdb:370070"}))
		if !errors.Is(err, ErrConflictingTerms) {
			t.Fatalf("error = %v, want ErrConflictingTerms", err)
		}
	})

	t.Run("too many terms error", func(t *testing.T) {
		source := &strategyFakeSource{}
		resolver := New(NewTextSearchStrategy(source))
		raw := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}
		_, err := resolver.Resolve(context.Background(), ParseQuery(raw))
		if !errors.Is(err, ErrTooManyTerms) {
			t.Fatalf("error = %v, want ErrTooManyTerms", err)
		}
	})
}
