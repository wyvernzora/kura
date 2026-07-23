package ingest

import (
	"context"
	"errors"
	"testing"

	"github.com/wyvernzora/kura/services/release-indexer/internal/store"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/rawpost"
)

const testInfohash = "0123456789abcdef0123456789abcdef01234567"

func TestProcessorBatch(t *testing.T) {
	s := &fakeStore{
		outcomes: []store.IngestOutcome{
			{New: true},
			{Duplicate: true},
		},
	}
	p := New(s, nil)

	batch, err := p.Batch(t.Context(), []rawpost.RawPost{
		post("one", "magnet:?xt=urn:btih:"+testInfohash),
		post("two", "magnet:?xt=urn:btih:"+testInfohash),
		post("v2", "magnet:?xt=urn:btmh:1220abcdef"),
	})
	if err != nil {
		t.Fatalf("Batch() error = %v", err)
	}
	if batch.New != 1 || batch.Duplicate != 1 || batch.Skipped != 1 {
		t.Fatalf("Batch() = %+v", batch)
	}
	if len(s.params) != 2 || s.params[0].Infohash != testInfohash {
		t.Fatalf("IngestN params = %+v", s.params)
	}
}

func TestProcessorBatchReportsFailingPost(t *testing.T) {
	wantErr := errors.New("store unavailable")
	s := &fakeStore{err: wantErr}
	p := New(s, nil)

	_, err := p.Batch(t.Context(), []rawpost.RawPost{post("source-7", "magnet:?xt=urn:btih:"+testInfohash)})
	var failure *Failure
	if !errors.As(err, &failure) {
		t.Fatalf("Batch() error = %v, want *Failure", err)
	}
	if failure.Index != 0 || failure.SourceID != "source-7" || !errors.Is(err, wantErr) {
		t.Fatalf("failure = %+v", failure)
	}
}

func post(sourceID, magnet string) rawpost.RawPost {
	return rawpost.RawPost{
		Source:   rawpost.SourceDMHY,
		SourceID: sourceID,
		Title:    sourceID,
		Magnet:   magnet,
	}
}

type fakeStore struct {
	outcomes []store.IngestOutcome
	params   []store.IngestParams
	err      error
}

func (s *fakeStore) IngestN(_ context.Context, p store.IngestParams) (store.IngestOutcome, error) {
	s.params = append(s.params, p)
	if s.err != nil {
		return store.IngestOutcome{}, s.err
	}
	out := s.outcomes[len(s.params)-1]
	return out, nil
}
