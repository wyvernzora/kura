package tvdb

import (
	"context"
	"net/http"
	"testing"

	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/textnorm"
)

func TestGetSeriesAggregatesExtendedAndEpisodes(t *testing.T) {
	server := newTestServer(t, nil)
	defer server.Close()

	p, err := New("test-key", Options{
		BaseURL:            server.URL,
		HTTPClient:         server.Client(),
		PreferredLanguages: []string{"ja", "en"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	series, err := p.GetSeries(context.Background(), "370070")
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}

	if series.MetadataRef != "tvdb:370070" {
		t.Fatalf("MetadataRef = %q, want tvdb:370070", series.MetadataRef)
	}
	if series.CanonicalTitle.String() != "Ascendance of a Bookworm" {
		t.Fatalf("CanonicalTitle = %q", series.CanonicalTitle)
	}
	if series.PreferredTitle.String() != "本好きの下剋上" {
		t.Fatalf("PreferredTitle = %q, want ja title", series.PreferredTitle)
	}
	if series.OriginalLanguage != "ja" {
		t.Fatalf("OriginalLanguage = %q, want ja", series.OriginalLanguage)
	}
	if series.OriginalCountry != "JP" {
		t.Fatalf("OriginalCountry = %q, want JP", series.OriginalCountry)
	}
	if series.FirstAired != "2019-10-03" || series.LastAired != "2022-06-14" {
		t.Fatalf("FirstAired/LastAired = %q/%q, want 2019-10-03/2022-06-14", series.FirstAired, series.LastAired)
	}
	if len(series.Seasons) != 2 {
		t.Fatalf("len(Seasons) = %d, want 2", len(series.Seasons))
	}
	if series.Seasons[0].Number != 0 {
		t.Fatalf("season number = %d, want 0", series.Seasons[0].Number)
	}
	if series.Seasons[1].Number != 1 {
		t.Fatalf("season number = %d, want 1", series.Seasons[1].Number)
	}
	episodeOne, err := refs.NewEpisode(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	specialOne, err := refs.NewEpisode(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := series.Seasons[1].Episodes[0]; got.MetadataRef != "tvdb:1001" || got.Ref != episodeOne {
		t.Fatalf("first season 1 episode = %#v", got)
	}
	if series.Seasons[1].Episodes[0].AbsoluteNumber == nil || *series.Seasons[1].Episodes[0].AbsoluteNumber != 1 {
		t.Fatalf("AbsoluteNumber = %#v, want 1", series.Seasons[1].Episodes[0].AbsoluteNumber)
	}
	if got := series.Seasons[1].Episodes[0].Aired; got != "2019-10-03" {
		t.Fatalf("Aired = %q, want 2019-10-03", got)
	}
	if len(series.Seasons[0].Episodes) != 1 {
		t.Fatalf("len(Seasons[0].Episodes) = %d, want 1", len(series.Seasons[0].Episodes))
	}
	if got := series.Seasons[0].Episodes[0]; got.MetadataRef != "tvdb:9001" || got.Ref != specialOne {
		t.Fatalf("first special = %#v", got)
	}
}

func TestSelectTitleUsesCanonicalAsOriginalLanguageFallback(t *testing.T) {
	p, err := New("test-key", Options{
		PreferredLanguages: []string{"ja", "en"},
		HTTPClient:         http.DefaultClient,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	title := p.selectTitle(textnorm.NFC("日本語タイトル"), "jpn", []titleCandidate{
		{Language: "eng", Value: "English Title"},
	})

	if title.String() != "日本語タイトル" {
		t.Fatalf("title = %q, want canonical ja fallback", title)
	}
}

func TestSelectTitlePrefersExplicitOriginalLanguageTranslation(t *testing.T) {
	p, err := New("test-key", Options{
		PreferredLanguages: []string{"ja", "en"},
		HTTPClient:         http.DefaultClient,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	title := p.selectTitle(textnorm.NFC("Provider Canonical"), "jpn", []titleCandidate{
		{Language: "jpn", Value: "日本語タイトル"},
		{Language: "eng", Value: "English Title"},
	})

	if title.String() != "日本語タイトル" {
		t.Fatalf("title = %q, want explicit ja translation", title)
	}
}

func TestSelectTitleFallsBackToNextPreferredLanguage(t *testing.T) {
	p, err := New("test-key", Options{
		PreferredLanguages: []string{"ja", "en"},
		HTTPClient:         http.DefaultClient,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	title := p.selectTitle(textnorm.NFC("Provider Canonical"), "eng", []titleCandidate{
		{Language: "eng", Value: "English Title"},
	})

	if title.String() != "English Title" {
		t.Fatalf("title = %q, want en translation", title)
	}
}

func TestNormalizeSeriesSummaryNormalizesProviderTitlesToNFC(t *testing.T) {
	p, err := New("test-key", Options{
		PreferredLanguages: []string{"ja"},
		HTTPClient:         http.DefaultClient,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	summary := p.normalizeSeriesSummary(seriesSummaryInput{
		ref:              "tvdb:1",
		canonicalTitle:   "本好きの下剋上 司書になるためには手段を選んでいられません",
		originalLanguage: "jpn",
		originalCountry:  "JP",
		firstAired:       "2019-10-03",
		status:           metadata.SeriesStatusContinuing,
		year:             2019,
		titles: []titleCandidate{
			{Language: "jpn", Value: "本好きの下剋上 司書になるためには手段を選んでいられません"},
		},
	})

	want := "本好きの下剋上 司書になるためには手段を選んでいられません"
	if summary.PreferredTitle.String() != want {
		t.Fatalf("PreferredTitle = %q, want %q", summary.PreferredTitle, want)
	}
	if summary.CanonicalTitle.String() != want {
		t.Fatalf("CanonicalTitle = %q, want %q", summary.CanonicalTitle, want)
	}
}
