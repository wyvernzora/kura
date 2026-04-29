package ui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/resolve"
	"github.com/wyvernzora/kura/internal/ui/stdio"
)

func TestResolveSeriesReturnsSingleResult(t *testing.T) {
	src := newResolveFakeSource()
	ctx := resolveSeriesTestContext(src)

	series, err := ResolveSeries(ctx, []string{"honzuki"})
	if err != nil {
		t.Fatalf("ResolveSeries: %v", err)
	}
	if series.MetadataRef != "tvdb:370070" {
		t.Fatalf("MetadataRef = %q, want tvdb:370070", series.MetadataRef)
	}
}

func TestResolveSeriesReturnsErrNoMetadataMatch(t *testing.T) {
	src := newResolveFakeSource()
	src.searchResults = nil
	ctx := resolveSeriesTestContext(src)

	_, err := ResolveSeries(ctx, []string{"missing"})
	if !errors.Is(err, ErrNoMetadataMatch) {
		t.Fatalf("error = %v, want ErrNoMetadataMatch", err)
	}
}

func TestResolveSeriesReturnsErrSelectionRequiredWhenNonInteractive(t *testing.T) {
	src := newResolveFakeSource()
	src.searchResults = append(src.searchResults, metadata.SearchResult{
		SeriesSummary: metadata.SeriesSummary{
			MetadataRef:    "tvdb:999999",
			PreferredTitle: "Other Bookworm",
			Year:           2021,
		},
	})
	var stderr bytes.Buffer
	ctx := resolveSeriesTestContextWithStdio(src, stdio.New(strings.NewReader(""), &bytes.Buffer{}, &stderr))

	_, err := ResolveSeries(ctx, []string{"bookworm"})
	if !errors.Is(err, ErrSelectionRequired) {
		t.Fatalf("error = %v, want ErrSelectionRequired", err)
	}
	for _, want := range []string{"tvdb:370070", "tvdb:999999"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %s", stderr.String(), want)
		}
	}
}

func TestResolveSeriesPropagatesResolverError(t *testing.T) {
	wantErr := errors.New("search failed")
	src := newResolveFakeSource()
	src.searchErr = wantErr
	ctx := resolveSeriesTestContext(src)

	_, err := ResolveSeries(ctx, []string{"honzuki"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

func TestResolveSeriesRejectsForeignMetadataRefSource(t *testing.T) {
	src := newResolveFakeSource()
	src.searchResults = []metadata.SearchResult{
		{
			SeriesSummary: metadata.SeriesSummary{
				MetadataRef:    "imdb:tt123",
				PreferredTitle: "Foreign",
			},
		},
	}
	ctx := resolveSeriesTestContext(src)

	_, err := ResolveSeries(ctx, []string{"foreign"})
	if err == nil {
		t.Fatal("ResolveSeries returned nil error, want foreign ref error")
	}
	if !strings.Contains(err.Error(), "unsupported metadata ref source") {
		t.Fatalf("error = %v, want unsupported metadata ref source", err)
	}
}

type resolveFakeSource struct {
	key           string
	searchResults []metadata.SearchResult
	searchErr     error
	seriesByID    map[string]metadata.Series
	getErr        error
}

func newResolveFakeSource() *resolveFakeSource {
	series := metadata.Series{
		SeriesSummary: metadata.SeriesSummary{
			MetadataRef:    "tvdb:370070",
			PreferredTitle: "Ascendance of a Bookworm",
			CanonicalTitle: "Honzuki no Gekokujou",
			Year:           2019,
		},
	}
	return &resolveFakeSource{
		key: "tvdb",
		searchResults: []metadata.SearchResult{
			{SeriesSummary: series.SeriesSummary},
		},
		seriesByID: map[string]metadata.Series{"370070": series},
	}
}

func (s *resolveFakeSource) Key() string {
	return s.key
}

func (s *resolveFakeSource) Search(context.Context, string, metadata.SearchOptions) ([]metadata.SearchResult, error) {
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	return s.searchResults, nil
}

func (s *resolveFakeSource) GetSeries(_ context.Context, metadataID string) (metadata.Series, error) {
	if s.getErr != nil {
		return metadata.Series{}, s.getErr
	}
	series, ok := s.seriesByID[metadataID]
	if !ok {
		return metadata.Series{}, metadata.ErrNotFound
	}
	return series, nil
}

func resolveSeriesTestContext(src *resolveFakeSource) context.Context {
	return resolveSeriesTestContextWithStdio(src, stdio.New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}))
}

func resolveSeriesTestContextWithStdio(src *resolveFakeSource, s stdio.Stdio) context.Context {
	ctx := context.Background()
	ctx = metadata.WithSource(ctx, func() (metadata.Source, error) {
		return src, nil
	})
	ctx = resolve.WithResolver(ctx, func() (*resolve.Resolver, error) {
		return resolve.New(resolve.NewMetadataIDStrategy(src), resolve.NewTextSearchStrategy(src)), nil
	})
	return stdio.With(ctx, s)
}
