package resolve

import (
	"context"
	"errors"
	"slices"
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
		hits: []TermHit{{
			ProviderRef: "tvdb:1",
			Summary:     testSummary("tvdb:1"),
		}},
	}}
	resolver := New(strategy)

	result, err := resolver.Resolve(context.Background(), Query{Terms: []Term{
		{},
		{Value: "   "},
		{Value: "Bookworm"},
	}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if calls := strategy.calls.Load(); calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if !result.IsResolved() {
		t.Fatalf("IsResolved = false, results = %#v", result.Results)
	}
}

func TestResolverAllEmptyValuedTermsAreEmptyQuery(t *testing.T) {
	strategy := &countingStrategy{fakeStrategy: fakeStrategy{match: true}}
	resolver := New(strategy)

	_, err := resolver.Resolve(context.Background(), Query{Terms: []Term{
		{},
		{Prefix: "tvdb"},
		{Value: "   "},
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
		query.Terms[i] = Term{Value: "term"}
	}
	_, err := resolver.Resolve(context.Background(), query)
	if !errors.Is(err, ErrTooManyTerms) {
		t.Fatalf("error = %v, want ErrTooManyTerms", err)
	}
}

func TestResolverNoStrategyMatch(t *testing.T) {
	resolver := New(fakeStrategy{})
	_, err := resolver.Resolve(context.Background(), Query{Terms: []Term{{Prefix: "unknown", Value: "1"}}})
	if !errors.Is(err, ErrNoStrategyMatch) {
		t.Fatalf("error = %v, want ErrNoStrategyMatch", err)
	}
}

func TestResolverConflictingAuthoritativeTerms(t *testing.T) {
	resolver := New(fakeStrategy{match: true, authoritative: true})
	_, err := resolver.Resolve(context.Background(), Query{Terms: []Term{
		{Prefix: "tvdb", Value: "1"},
		{Prefix: "tvdb", Value: "2"},
	}})
	if !errors.Is(err, ErrConflictingTerms) {
		t.Fatalf("error = %v, want ErrConflictingTerms", err)
	}
}

func TestResolverDuplicateAuthoritativeTermCollapses(t *testing.T) {
	strategy := &countingStrategy{fakeStrategy: fakeStrategy{
		match:         true,
		authoritative: true,
		hits: []TermHit{{
			ProviderRef: "tvdb:1",
			Summary:     testSummary("tvdb:1"),
		}},
	}}
	resolver := New(strategy)

	result, err := resolver.Resolve(context.Background(), Query{Terms: []Term{
		{Prefix: "tvdb", Value: "1"},
		{Prefix: "tvdb", Value: "1"},
	}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if calls := strategy.calls.Load(); calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if !result.IsResolved() {
		t.Fatalf("IsResolved = false, results = %#v", result.Results)
	}
}

func TestResolverAuthoritativeAndNonAuthoritativeConflict(t *testing.T) {
	resolver := New(
		fakeStrategy{matchPrefix: "tvdb", authoritative: true},
		fakeStrategy{matchPrefix: "", matchEmptyPrefix: true},
	)
	_, err := resolver.Resolve(context.Background(), Query{Terms: []Term{
		{Prefix: "tvdb", Value: "1"},
		{Value: "Bookworm"},
	}})
	if !errors.Is(err, ErrConflictingTerms) {
		t.Fatalf("error = %v, want ErrConflictingTerms", err)
	}
}

func TestResolverAggregatesSameRemoteRef(t *testing.T) {
	resolver := New(fakeStrategy{
		match: true,
		hitsForTerm: map[Term][]TermHit{
			{Value: "jp"}: {{
				Term:        Term{Value: "jp"},
				ProviderRef: "tvdb:1",
				Summary:     testSummary("tvdb:1"),
				Rank:        0,
			}},
			{Value: "en"}: {{
				Term:        Term{Value: "en"},
				ProviderRef: "tvdb:1",
				Summary:     testSummary("tvdb:1"),
				Rank:        1,
			}},
		},
	})

	result, err := resolver.Resolve(context.Background(), Query{Terms: []Term{{Value: "jp"}, {Value: "en"}}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !result.IsResolved() {
		t.Fatalf("IsResolved = false, results = %#v", result.Results)
	}
	if len(result.Results[0].Evidence) != 2 {
		t.Fatalf("evidence count = %d, want 2", len(result.Results[0].Evidence))
	}
}

func TestResolverUnresolvedDistinctRemoteRefs(t *testing.T) {
	resolver := New(fakeStrategy{
		match: true,
		hits: []TermHit{
			{ProviderRef: "tvdb:1", Summary: testSummary("tvdb:1")},
			{ProviderRef: "tvdb:2", Summary: testSummary("tvdb:2")},
			{ProviderRef: "tvdb:3", Summary: testSummary("tvdb:3")},
		},
	})

	result, err := resolver.Resolve(context.Background(), Query{Terms: []Term{{Value: "query"}}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !result.IsUnresolved() {
		t.Fatalf("IsUnresolved = false, results = %#v", result.Results)
	}
}

func TestResolverNotFound(t *testing.T) {
	resolver := New(fakeStrategy{match: true})
	result, err := resolver.Resolve(context.Background(), Query{Terms: []Term{{Value: "missing"}}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !result.IsNotFound() {
		t.Fatalf("IsNotFound = false, results = %#v", result.Results)
	}
}

func TestResolverPropagatesErrorAndCancelsSiblings(t *testing.T) {
	ready := make(chan struct{})
	cancelled := make(chan struct{})
	blocking := fakeStrategy{
		matchPrefix: "wait",
		resolveFunc: func(ctx context.Context, _ Term) ([]TermHit, error) {
			close(ready)
			<-ctx.Done()
			close(cancelled)
			return nil, ctx.Err()
		},
	}
	failing := fakeStrategy{
		matchPrefix: "fail",
		resolveFunc: func(context.Context, Term) ([]TermHit, error) {
			<-ready
			return nil, errors.New("boom")
		},
	}
	resolver := New(blocking, failing)

	_, err := resolver.Resolve(context.Background(), Query{Terms: []Term{
		{Prefix: "wait", Value: "1"},
		{Prefix: "fail", Value: "2"},
	}})
	if err == nil {
		t.Fatal("Resolve error = nil, want propagated error")
	}
	<-cancelled
}

func TestResolverSortOrder(t *testing.T) {
	resolver := New(fakeStrategy{
		match: true,
		hitsForTerm: map[Term][]TermHit{
			{Value: "a"}: {
				{ProviderRef: "tvdb:1", Summary: testSummary("tvdb:1"), Rank: 0},
				{ProviderRef: "tvdb:2", Summary: testSummary("tvdb:2"), Rank: 1},
				{ProviderRef: "tvdb:3", Summary: testSummary("tvdb:3"), Rank: 0},
			},
			{Value: "b"}: {
				{ProviderRef: "tvdb:2", Summary: testSummary("tvdb:2"), Rank: 3},
				{ProviderRef: "tvdb:3", Summary: testSummary("tvdb:3"), Rank: 1},
			},
			{Value: "c"}: {
				{ProviderRef: "tvdb:4", Summary: testSummary("tvdb:4"), Rank: 0},
			},
		},
	})

	result, err := resolver.Resolve(context.Background(), Query{Terms: []Term{{Value: "a"}, {Value: "b"}, {Value: "c"}}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	var refs []string
	for _, candidate := range result.Results {
		refs = append(refs, candidate.Summary.ProviderRef)
	}
	want := []string{"tvdb:3", "tvdb:2", "tvdb:1", "tvdb:4"}
	if !slices.Equal(refs, want) {
		t.Fatalf("refs = %#v, want %#v", refs, want)
	}
}

type fakeStrategy struct {
	name             string
	match            bool
	matchPrefix      string
	matchEmptyPrefix bool
	authoritative    bool
	hits             []TermHit
	hitsForTerm      map[Term][]TermHit
	err              error
	resolveFunc      func(context.Context, Term) ([]TermHit, error)
}

func (s fakeStrategy) Name() string {
	if s.name != "" {
		return s.name
	}
	return "fake"
}

func (s fakeStrategy) Match(term Term) bool {
	if s.match {
		return true
	}
	if s.matchEmptyPrefix && term.Prefix == "" {
		return true
	}
	return s.matchPrefix != "" && term.Prefix == s.matchPrefix
}

func (s fakeStrategy) Authoritative() bool {
	return s.authoritative
}

func (s fakeStrategy) Resolve(ctx context.Context, term Term) ([]TermHit, error) {
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

func (s *countingStrategy) Resolve(ctx context.Context, term Term) ([]TermHit, error) {
	s.calls.Add(1)
	return s.fakeStrategy.Resolve(ctx, term)
}
