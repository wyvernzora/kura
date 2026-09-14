package resolve

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/selector"
	"github.com/wyvernzora/kura/services/library-manager/internal/provider"
	"github.com/wyvernzora/kura/services/library-manager/internal/textnorm"
)

func TestTextSearchStrategyResolveEmpty(t *testing.T) {
	strategy := NewTextSearchStrategy(&strategyFakeSource{})
	hits, err := strategy.Resolve(context.Background(), selector.Term("missing"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("len(hits) = %d, want 0", len(hits))
	}
}

func TestTextSearchStrategyResolveOne(t *testing.T) {
	strategy := NewTextSearchStrategy(&strategyFakeSource{
		searchResults: []provider.SearchResult{{
			SeriesSummary: testSummary("tvdb:1"),
			MatchSource:   "title",
		}},
	})
	hits, err := strategy.Resolve(context.Background(), selector.Term("query"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("len(hits) = %d, want 1", len(hits))
	}
	if hits[0].Rank != 0 || hits[0].MetadataRef != "tvdb:1" {
		t.Fatalf("hit = %#v, want rank 0 tvdb:1", hits[0])
	}
	if hits[0].MatchSource != "title" {
		t.Fatalf("MatchSource = %q, want title", hits[0].MatchSource)
	}
}

func TestTextSearchStrategyAddsMatchAnnotations(t *testing.T) {
	strategy := NewTextSearchStrategy(&strategyFakeSource{
		searchResults: []provider.SearchResult{{
			SeriesSummary: provider.SeriesSummary{
				MetadataRef:    "tvdb:1",
				PreferredTitle: textnorm.NFC("Ascendance of a Bookworm"),
				CanonicalTitle: textnorm.NFC("本好きの下剋上"),
			},
			Aliases: []textnorm.NFCString{
				textnorm.NFC("Ascendance of a Bookworm"),
				textnorm.NFC("本好きの下剋上"),
				textnorm.NFC("Honzuki no Gekokujou"),
			},
		}},
	})

	full, err := strategy.Resolve(context.Background(), selector.Term("本好きの下剋上"))
	if err != nil {
		t.Fatalf("Resolve full: %v", err)
	}
	if len(full) != 1 || !slices.Equal(full[0].Annotations, []string{"full_match"}) {
		t.Fatalf("full annotations = %#v, want full_match", full)
	}

	token, err := strategy.Resolve(context.Background(), selector.Term("bookworm"))
	if err != nil {
		t.Fatalf("Resolve token: %v", err)
	}
	if len(token) != 1 || !slices.Equal(token[0].Annotations, []string{"token_match"}) {
		t.Fatalf("token annotations = %#v, want token_match", token)
	}

	// Substring that crosses no word boundary: weaker than a whole-word
	// hit, so it must stay in the partial tier.
	partial, err := strategy.Resolve(context.Background(), selector.Term("好きの下"))
	if err != nil {
		t.Fatalf("Resolve partial: %v", err)
	}
	if len(partial) != 1 || !slices.Equal(partial[0].Annotations, []string{"partial_match"}) {
		t.Fatalf("partial annotations = %#v, want partial_match", partial)
	}

	none, err := strategy.Resolve(context.Background(), selector.Term("frieren"))
	if err != nil {
		t.Fatalf("Resolve none: %v", err)
	}
	if len(none) != 1 || len(none[0].Annotations) != 0 {
		t.Fatalf("unmatched annotations = %#v, want none", none)
	}
}

// TestResolveRanksMatchQualityAboveProviderRank is the regression for
// TVDB search burying the obvious answer. Each case is the real head of
// a TVDB /search response (aliases verbatim) for the named query: the
// resolver previously emitted them in provider order, so "frieren"
// listed the 2020 Swiss drama "Frieden" first and put the actual
// Frieren nineteenth.
func TestResolveRanksMatchQualityAboveProviderRank(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		results []provider.SearchResult
		want    refs.Metadata
	}{
		{
			name:  "exact title match outranks unrelated substrings",
			query: "frieren",
			results: []provider.SearchResult{
				searchResult("tvdb:1", "Frieden"),
				searchResult("tvdb:2", "Friheden"),
				searchResult("tvdb:3", "Der vergiftete Frieden"),
				searchResult("tvdb:4", "葬送のフリーレン", "Sousou no Frieren", "Frieren", "Frieren: Beyond Journey's End"),
			},
			want: "tvdb:4",
		},
		{
			name:  "whole-word alias hit outranks coincidental substrings",
			query: "bocchi",
			results: []provider.SearchResult{
				searchResult("tvdb:1", "あっちこっち", "Acchi Kocchi", "Place to Place"),
				searchResult("tvdb:2", "Luce dei tuoi occhi"),
				searchResult("tvdb:3", "ぼっち・ざ・ろっく！", "Botchi za Rokku!", "Bocchi the Rock!"),
			},
			want: "tvdb:3",
		},
		{
			name:  "punctuation-insensitive exact match beats a superset title",
			query: "spy family",
			results: []provider.SearchResult{
				searchResult("tvdb:1", "SPY×FAMILY", "Spy x Family", "スパイファミリー"),
				searchResult("tvdb:2", "My Spy Family"),
			},
			want: "tvdb:1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolver := New(NewTextSearchStrategy(&strategyFakeSource{searchResults: tc.results}))
			res, err := resolver.Resolve(context.Background(), selector.Selector{Terms: []selector.Term{selector.Term(tc.query)}})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if len(res.Results) != len(tc.results) {
				t.Fatalf("len(results) = %d, want %d", len(res.Results), len(tc.results))
			}
			if got := res.Results[0].Summary.MetadataRef; got != tc.want {
				t.Fatalf("first result = %q, want %q", got, tc.want)
			}
		})
	}
}

// searchResult builds a candidate whose first title is both its
// preferred title and its first alias, mirroring how the TVDB search
// normalizer folds `name` into the alias list.
func searchResult(ref string, titles ...string) provider.SearchResult {
	aliases := make([]textnorm.NFCString, 0, len(titles))
	for _, title := range titles {
		aliases = append(aliases, textnorm.NFC(title))
	}
	return provider.SearchResult{
		SeriesSummary: provider.SeriesSummary{
			MetadataRef:    refs.Metadata(ref),
			PreferredTitle: textnorm.NFC(titles[0]),
		},
		Aliases: aliases,
	}
}

func TestTextSearchStrategyResolveRanks(t *testing.T) {
	strategy := NewTextSearchStrategy(&strategyFakeSource{
		searchResults: []provider.SearchResult{
			{SeriesSummary: testSummary("tvdb:1")},
			{SeriesSummary: testSummary("tvdb:2")},
			{SeriesSummary: testSummary("tvdb:3")},
		},
	})
	hits, err := strategy.Resolve(context.Background(), selector.Term("query"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for i, hit := range hits {
		if hit.Rank != i {
			t.Fatalf("hit[%d].Rank = %d, want %d", i, hit.Rank, i)
		}
	}
}

func TestTextSearchStrategyPropagatesError(t *testing.T) {
	strategy := NewTextSearchStrategy(&strategyFakeSource{searchErr: provider.ErrUnauthorized})
	_, err := strategy.Resolve(context.Background(), selector.Term("query"))
	if !errors.Is(err, provider.ErrUnauthorized) {
		t.Fatalf("error = %v, want ErrUnauthorized", err)
	}
}

func TestTextSearchStrategyNotFound(t *testing.T) {
	strategy := NewTextSearchStrategy(&strategyFakeSource{searchErr: provider.ErrNotFound})
	hits, err := strategy.Resolve(context.Background(), selector.Term("query"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("len(hits) = %d, want 0", len(hits))
	}
}

func TestTextSearchStrategyProperties(t *testing.T) {
	strategy := NewTextSearchStrategy(&strategyFakeSource{})
	matched, stop := strategy.Match(selector.Term("query"))
	if !matched {
		t.Fatal("Match text = false, want true")
	}
	if stop {
		t.Fatal("Match text stop = true, want false")
	}
	matched, stop = strategy.Match(selector.Term("unknown:Bookworm"))
	if !matched {
		t.Fatal("Match prefixed = false, want true")
	}
	if stop {
		t.Fatal("Match prefixed stop = true, want false")
	}
	if strategy.Authoritative() {
		t.Fatal("Authoritative = true, want false")
	}
}
