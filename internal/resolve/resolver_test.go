package resolve

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func TestResolverEmptyQuery(t *testing.T) {
	resolver := New(fakeStrategy{match: true})
	_, err := resolver.Resolve(context.Background(), Query{})
	if !errors.Is(err, ErrEmptyQuery) {
		t.Fatalf("error = %v, want ErrEmptyQuery", err)
	}
}

func TestResolverEmptyValuedTermsAreIgnored(t *testing.T) {
	strategy := &countingStrategy{fakeStrategy: fakeStrategy{
		match: true,
		hits: []termHit{{
			MetadataRef: "tvdb:1",
			Summary:     testSummary("tvdb:1"),
		}},
	}}
	resolver := New(strategy)

	result, err := resolver.Resolve(context.Background(), Query{Terms: []Term{
		Term(""),
		Term("   "),
		Term("Bookworm"),
	}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if calls := strategy.calls.Load(); calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if len(result.Results) != 1 {
		t.Fatalf("len(Results) = %d, want 1", len(result.Results))
	}
}

func TestResolverAllEmptyValuedTermsAreEmptyQuery(t *testing.T) {
	strategy := &countingStrategy{fakeStrategy: fakeStrategy{match: true}}
	resolver := New(strategy)

	_, err := resolver.Resolve(context.Background(), Query{Terms: []Term{
		Term(""),
		Term("   "),
		Term("   "),
	}})
	if !errors.Is(err, ErrEmptyQuery) {
		t.Fatalf("error = %v, want ErrEmptyQuery", err)
	}
	if calls := strategy.calls.Load(); calls != 0 {
		t.Fatalf("calls = %d, want 0", calls)
	}
}

func TestResolverTooManyTerms(t *testing.T) {
	resolver := New(fakeStrategy{match: true})
	query := Query{Terms: make([]Term, MaxTerms+1)}
	for i := range query.Terms {
		query.Terms[i] = Term("term")
	}
	_, err := resolver.Resolve(context.Background(), query)
	if !errors.Is(err, ErrTooManyTerms) {
		t.Fatalf("error = %v, want ErrTooManyTerms", err)
	}
}

func TestResolverNoStrategyMatchWithoutFallback(t *testing.T) {
	resolver := New(fakeStrategy{})
	_, err := resolver.Resolve(context.Background(), Query{Terms: []Term{Term("unknown:1")}})
	if !errors.Is(err, ErrNoStrategyMatch) {
		t.Fatalf("error = %v, want ErrNoStrategyMatch", err)
	}
}

func TestResolverRunsMultipleMatchingStrategiesForSameTerm(t *testing.T) {
	first := &countingStrategy{fakeStrategy: fakeStrategy{
		name:  "first",
		match: true,
		hits: []termHit{{
			Term:        Term("Bookworm"),
			MetadataRef: "tvdb:1",
			Summary:     testSummary("tvdb:1"),
			MatchSource: "first",
		}},
	}}
	second := &countingStrategy{fakeStrategy: fakeStrategy{
		name:  "second",
		match: true,
		hits: []termHit{{
			Term:        Term("Bookworm"),
			MetadataRef: "tvdb:2",
			Summary:     testSummary("tvdb:2"),
			MatchSource: "second",
		}},
	}}
	resolver := New(first, second)

	result, err := resolver.Resolve(context.Background(), Query{Terms: []Term{Term("Bookworm")}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if first.calls.Load() != 1 || second.calls.Load() != 1 {
		t.Fatalf("calls = (%d, %d), want (1, 1)", first.calls.Load(), second.calls.Load())
	}
	if len(result.Results) != 2 {
		t.Fatalf("len(Results) = %d, want 2", len(result.Results))
	}
}

func TestResolverStopsMatchingAfterStoppingStrategy(t *testing.T) {
	first := &countingStrategy{fakeStrategy: fakeStrategy{
		name:  "first",
		match: true,
		stop:  true,
		hits: []termHit{{
			Term:        Term("tvdb:1"),
			MetadataRef: "tvdb:1",
			Summary:     testSummary("tvdb:1"),
		}},
	}}
	second := &countingStrategy{fakeStrategy: fakeStrategy{
		name:  "second",
		match: true,
		hits: []termHit{{
			Term:        Term("tvdb:1"),
			MetadataRef: "tvdb:2",
			Summary:     testSummary("tvdb:2"),
		}},
	}}
	resolver := New(first, second)

	result, err := resolver.Resolve(context.Background(), Query{Terms: []Term{Term("tvdb:1")}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if first.calls.Load() != 1 || second.calls.Load() != 0 {
		t.Fatalf("calls = (%d, %d), want (1, 0)", first.calls.Load(), second.calls.Load())
	}
	if len(result.Results) != 1 || result.Results[0].Summary.MetadataRef != "tvdb:1" {
		t.Fatalf("results = %#v, want only tvdb:1", result.Results)
	}
}

func TestResolverConflictingAuthoritativeTerms(t *testing.T) {
	resolver := New(fakeStrategy{match: true, authoritative: true})
	_, err := resolver.Resolve(context.Background(), Query{Terms: []Term{
		Term("tvdb:1"),
		Term("tvdb:2"),
	}})
	if !errors.Is(err, ErrConflictingTerms) {
		t.Fatalf("error = %v, want ErrConflictingTerms", err)
	}
}

func TestResolverDuplicateAuthoritativeTermCollapses(t *testing.T) {
	strategy := &countingStrategy{fakeStrategy: fakeStrategy{
		match:         true,
		authoritative: true,
		hits: []termHit{{
			MetadataRef: "tvdb:1",
			Summary:     testSummary("tvdb:1"),
		}},
	}}
	resolver := New(strategy)

	result, err := resolver.Resolve(context.Background(), Query{Terms: []Term{
		Term("tvdb:1"),
		Term("tvdb:1"),
	}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if calls := strategy.calls.Load(); calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if len(result.Results) != 1 {
		t.Fatalf("len(Results) = %d, want 1", len(result.Results))
	}
}

func TestResolverAuthoritativeAndNonAuthoritativeConflict(t *testing.T) {
	resolver := New(
		fakeStrategy{matchPrefix: "tvdb", authoritative: true},
		fakeStrategy{matchPrefix: "", matchEmptyPrefix: true},
	)
	_, err := resolver.Resolve(context.Background(), Query{Terms: []Term{
		Term("tvdb:1"),
		Term("Bookworm"),
	}})
	if !errors.Is(err, ErrConflictingTerms) {
		t.Fatalf("error = %v, want ErrConflictingTerms", err)
	}
}

func TestResolverAggregatesSameRemoteRef(t *testing.T) {
	resolver := New(fakeStrategy{
		match: true,
		hitsForTerm: map[Term][]termHit{
			Term("jp"): {{
				Term:        Term("jp"),
				MetadataRef: "tvdb:1",
				Summary:     testSummary("tvdb:1"),
				Rank:        0,
			}},
			Term("en"): {{
				Term:        Term("en"),
				MetadataRef: "tvdb:1",
				Summary:     testSummary("tvdb:1"),
				Rank:        1,
			}},
		},
	})

	result, err := resolver.Resolve(context.Background(), Query{Terms: []Term{Term("jp"), Term("en")}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(result.Results) != 1 {
		t.Fatalf("len(Results) = %d, want 1", len(result.Results))
	}
	if len(result.Results[0].Evidence) != 2 {
		t.Fatalf("evidence count = %d, want 2", len(result.Results[0].Evidence))
	}
	if !slices.ContainsFunc(result.Results[0].Evidence, func(e Evidence) bool {
		return e.Term == "jp" && e.Rank == 0
	}) {
		t.Fatalf("evidence = %#v, want jp rank 0", result.Results[0].Evidence)
	}
}

func TestResolverUnresolvedDistinctRemoteRefs(t *testing.T) {
	resolver := New(fakeStrategy{
		match: true,
		hits: []termHit{
			{MetadataRef: "tvdb:1", Summary: testSummary("tvdb:1")},
			{MetadataRef: "tvdb:2", Summary: testSummary("tvdb:2")},
			{MetadataRef: "tvdb:3", Summary: testSummary("tvdb:3")},
		},
	})

	result, err := resolver.Resolve(context.Background(), Query{Terms: []Term{Term("query")}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(result.Results) <= 1 {
		t.Fatalf("len(Results) = %d, want >1", len(result.Results))
	}
}

func TestResolverNotFound(t *testing.T) {
	resolver := New(fakeStrategy{match: true})
	result, err := resolver.Resolve(context.Background(), Query{Terms: []Term{Term("missing")}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(result.Results) != 0 {
		t.Fatalf("len(Results) = %d, want 0", len(result.Results))
	}
}

func TestResolverPropagatesErrorAndCancelsSiblings(t *testing.T) {
	ready := make(chan struct{})
	cancelled := make(chan struct{})
	blocking := fakeStrategy{
		matchPrefix: "wait",
		resolveFunc: func(ctx context.Context, _ Term) ([]termHit, error) {
			close(ready)
			<-ctx.Done()
			close(cancelled)
			return nil, ctx.Err()
		},
	}
	failing := fakeStrategy{
		matchPrefix: "fail",
		resolveFunc: func(context.Context, Term) ([]termHit, error) {
			<-ready
			return nil, errors.New("boom")
		},
	}
	resolver := New(blocking, failing)

	_, err := resolver.Resolve(context.Background(), Query{Terms: []Term{
		Term("wait:1"),
		Term("fail:2"),
	}})
	if err == nil {
		t.Fatal("Resolve error = nil, want propagated error")
	}
	<-cancelled
}

func TestResolverSortOrder(t *testing.T) {
	resolver := New(fakeStrategy{
		match: true,
		hitsForTerm: map[Term][]termHit{
			Term("a"): {
				{MetadataRef: "tvdb:1", Summary: testSummary("tvdb:1"), Rank: 0},
				{MetadataRef: "tvdb:2", Summary: testSummary("tvdb:2"), Rank: 1},
				{MetadataRef: "tvdb:3", Summary: testSummary("tvdb:3"), Rank: 0},
			},
			Term("b"): {
				{MetadataRef: "tvdb:2", Summary: testSummary("tvdb:2"), Rank: 3},
				{MetadataRef: "tvdb:3", Summary: testSummary("tvdb:3"), Rank: 1},
			},
			Term("c"): {
				{MetadataRef: "tvdb:4", Summary: testSummary("tvdb:4"), Rank: 0},
			},
		},
	})

	result, err := resolver.Resolve(context.Background(), Query{Terms: []Term{Term("a"), Term("b"), Term("c")}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	var refs []string
	for _, candidate := range result.Results {
		refs = append(refs, candidate.Summary.MetadataRef.String())
	}
	want := []string{"tvdb:3", "tvdb:2", "tvdb:1", "tvdb:4"}
	if !slices.Equal(refs, want) {
		t.Fatalf("refs = %#v, want %#v", refs, want)
	}
}

type fakeStrategy struct {
	name             string
	match            bool
	stop             bool
	matchPrefix      string
	matchEmptyPrefix bool
	authoritative    bool
	hits             []termHit
	hitsForTerm      map[Term][]termHit
	err              error
	resolveFunc      func(context.Context, Term) ([]termHit, error)
}

func (s fakeStrategy) Name() string {
	if s.name != "" {
		return s.name
	}
	return "fake"
}

func (s fakeStrategy) Match(term Term) (bool, bool) {
	if s.match {
		return true, s.stop
	}
	prefix := termPrefix(term)
	if s.matchEmptyPrefix && prefix == "" {
		return true, s.stop
	}
	matched := s.matchPrefix != "" && prefix == s.matchPrefix
	return matched, matched && s.stop
}

func (s fakeStrategy) Authoritative() bool {
	return s.authoritative
}

func (s fakeStrategy) Resolve(ctx context.Context, term Term) ([]termHit, error) {
	if s.resolveFunc != nil {
		return s.resolveFunc(ctx, term)
	}
	if s.err != nil {
		return nil, s.err
	}
	if hits, ok := s.hitsForTerm[term]; ok {
		return slices.Clone(hits), nil
	}
	return slices.Clone(s.hits), nil
}

type countingStrategy struct {
	fakeStrategy
	calls atomic.Int64
}

func (s *countingStrategy) Resolve(ctx context.Context, term Term) ([]termHit, error) {
	s.calls.Add(1)
	return s.fakeStrategy.Resolve(ctx, term)
}

func termPrefix(term Term) string {
	prefix, _, ok := strings.Cut(term.String(), ":")
	if !ok {
		return ""
	}
	return prefix
}
