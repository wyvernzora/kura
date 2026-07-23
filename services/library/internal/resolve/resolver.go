package resolve

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library/internal/domain/selector"
	"golang.org/x/sync/errgroup"
)

const MaxTerms = 10

// Resolver runs queries against an immutable ordered list of strategies.
type Resolver struct {
	strategies []ResolveStrategy
}

// New constructs a resolver. Strategy order is match-priority order.
func New(strategies ...ResolveStrategy) *Resolver {
	if len(strategies) == 0 {
		panic("resolve: resolver requires at least one strategy")
	}
	return &Resolver{strategies: slices.Clone(strategies)}
}

type matchedTerm struct {
	term     selector.Term
	strategy ResolveStrategy
}

// Resolve runs the query and returns the merged, sorted result set.
func (r *Resolver) Resolve(ctx context.Context, q selector.Selector) (Resolution, error) {
	terms := nonEmptyTerms(q.Terms)
	if len(terms) == 0 {
		return Resolution{}, ErrEmptyQuery
	}
	if len(terms) > MaxTerms {
		return Resolution{}, ErrTooManyTerms
	}

	matched := make([]matchedTerm, 0, len(terms))
	for _, term := range terms {
		termMatches := r.matchStrategies(term)
		if len(termMatches) == 0 {
			return Resolution{}, fmt.Errorf("%w: %q", ErrNoStrategyMatch, term.String())
		}
		matched = append(matched, termMatches...)
	}
	if err := validateCombinations(matched); err != nil {
		return Resolution{}, err
	}

	matched = dedupeMatched(matched)
	var mu sync.Mutex
	var hits []termHit
	group, gctx := errgroup.WithContext(ctx)
	for _, entry := range matched {
		group.Go(func() error {
			resolved, err := entry.strategy.Resolve(gctx, entry.term)
			if err != nil {
				return err
			}
			mu.Lock()
			hits = append(hits, resolved...)
			mu.Unlock()
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return Resolution{}, err
	}

	resultsByRef := map[refs.Metadata]*Result{}
	for _, hit := range hits {
		result, ok := resultsByRef[hit.MetadataRef]
		if !ok {
			result = &Result{Summary: hit.Summary}
			resultsByRef[hit.MetadataRef] = result
		}
		result.Evidence = append(result.Evidence, Evidence{
			Term:        hit.Term.String(),
			Rank:        hit.Rank,
			MatchSource: hit.MatchSource,
			Annotations: slices.Clone(hit.Annotations),
		})
	}

	results := make([]Result, 0, len(resultsByRef))
	for _, result := range resultsByRef {
		results = append(results, *result)
	}
	slices.SortFunc(results, compareResults)
	return Resolution{Results: results}, nil
}

func nonEmptyTerms(terms []selector.Term) []selector.Term {
	out := make([]selector.Term, 0, len(terms))
	for _, term := range terms {
		if strings.TrimSpace(term.String()) == "" {
			continue
		}
		out = append(out, term)
	}
	return out
}

func (r *Resolver) matchStrategies(term selector.Term) []matchedTerm {
	var matched []matchedTerm
	for _, strategy := range r.strategies {
		ok, stop := strategy.Match(term)
		if ok {
			matched = append(matched, matchedTerm{term: term, strategy: strategy})
		}
		if stop {
			break
		}
	}
	return matched
}

func validateCombinations(matched []matchedTerm) error {
	deduped := map[selector.Term]struct{}{}
	hasAuthoritative := false
	for _, entry := range matched {
		deduped[entry.term] = struct{}{}
		if entry.strategy.Authoritative() {
			hasAuthoritative = true
		}
	}
	if hasAuthoritative && len(deduped) != 1 {
		return ErrConflictingTerms
	}
	return nil
}

func dedupeMatched(matched []matchedTerm) []matchedTerm {
	type key struct {
		term     selector.Term
		strategy string
	}
	seen := map[key]struct{}{}
	out := make([]matchedTerm, 0, len(matched))
	for _, entry := range matched {
		key := key{term: entry.term, strategy: entry.strategy.Name()}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, entry)
	}
	return out
}

func sumRank(hits []Evidence) int {
	sum := 0
	for _, hit := range hits {
		sum += hit.Rank
	}
	return sum
}

func minRank(hits []Evidence) int {
	if len(hits) == 0 {
		return 0
	}
	minimum := hits[0].Rank
	for _, hit := range hits[1:] {
		minimum = min(minimum, hit.Rank)
	}
	return minimum
}

func compareResults(a, b Result) int {
	if diff := len(b.Evidence) - len(a.Evidence); diff != 0 {
		return diff
	}
	if diff := sumRank(a.Evidence) - sumRank(b.Evidence); diff != 0 {
		return diff
	}
	if diff := minRank(a.Evidence) - minRank(b.Evidence); diff != 0 {
		return diff
	}
	return compareMetadataRef(a, b)
}

func compareMetadataRef(a, b Result) int {
	aRef := a.Summary.MetadataRef
	bRef := b.Summary.MetadataRef
	if aRef < bRef {
		return -1
	}
	if aRef > bRef {
		return 1
	}
	return 0
}
