package resolve

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/selector"
	"github.com/wyvernzora/kura/services/library-manager/internal/provider"
	"github.com/wyvernzora/kura/services/library-manager/internal/textnorm"
)

type textSearchStrategy struct {
	source provider.Source
}

func NewTextSearchStrategy(source provider.Source) ResolveStrategy {
	return &textSearchStrategy{source: source}
}

func (s *textSearchStrategy) Name() string {
	return "text_search"
}

func (s *textSearchStrategy) Match(t selector.Term) (matched bool, stop bool) {
	return true, false
}

func (s *textSearchStrategy) Authoritative() bool {
	return false
}

func (s *textSearchStrategy) Resolve(ctx context.Context, t selector.Term) ([]termHit, error) {
	query := textnorm.NFC(t.String())
	results, err := s.source.Search(ctx, query, provider.SearchOptions{Type: provider.MediaTypeSeries})
	if err != nil {
		if errors.Is(err, provider.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	hits := make([]termHit, 0, len(results))
	for i, result := range results {
		hits = append(hits, termHit{
			Term:        t,
			MetadataRef: result.MetadataRef,
			Summary:     result.SeriesSummary,
			Rank:        i,
			MatchSource: result.MatchSource,
			Annotations: matchAnnotations(query.String(), result),
		})
	}
	return hits, nil
}

// Match-quality annotations, best first. These drive candidate
// ordering: TVDB's own search ranking is title-substring-ish and buries
// the obvious answer (a "frieren" query returns the 2020 Swiss drama
// "Frieden" first, "Sousou no Frieren" tenth), so the resolver re-ranks
// by how well the query matches a candidate's title set and only falls
// back to provider rank as a tiebreak.
const (
	annotationFullMatch  = "full_match"
	annotationTokenMatch = "token_match"
	annotationPartial    = "partial_match"
)

// matchTier maps an annotation set to its ordering weight, lower being a
// better match. Candidates with no qualifying annotation sort last.
func matchTier(annotations []string) int {
	for _, annotation := range annotations {
		switch annotation {
		case annotationFullMatch:
			return 0
		case annotationTokenMatch:
			return 1
		case annotationPartial:
			return 2
		}
	}
	return 3
}

// matchAnnotations reports the best match this term makes against any of
// the candidate's titles: the whole folded title equal to the query,
// every query word present among the title's words, or the query as a
// raw substring. Folding drops punctuation so "spy family" matches
// "SPY×FAMILY" exactly; CJK titles fold to a single token and so are
// carried by the substring tier.
func matchAnnotations(term string, result provider.SearchResult) []string {
	queryTokens, queryKey := foldTitle(term)
	if queryKey == "" {
		return nil
	}
	best := ""
	for _, title := range matchTitles(result) {
		titleTokens, titleKey := foldTitle(title.String())
		if titleKey == "" {
			continue
		}
		switch {
		case titleKey == queryKey:
			return []string{annotationFullMatch}
		case containsAllTokens(titleTokens, queryTokens):
			best = annotationTokenMatch
		case best == "" && strings.Contains(titleKey, queryKey):
			best = annotationPartial
		}
	}
	if best == "" {
		return nil
	}
	return []string{best}
}

// foldTitle lowercases a title and reduces every non-alphanumeric rune
// to a separator, returning the word list and the space-joined form.
func foldTitle(value string) (tokens []string, key string) {
	folded := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, value)
	tokens = strings.Fields(folded)
	return tokens, strings.Join(tokens, " ")
}

func containsAllTokens(have, want []string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}

func matchTitles(result provider.SearchResult) []textnorm.NFCString {
	return result.Aliases
}
