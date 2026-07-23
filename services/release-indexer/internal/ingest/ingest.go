// Package ingest owns transport-neutral raw-post ingestion.
package ingest

import (
	"context"
	"errors"
	"fmt"

	"github.com/wyvernzora/kura/services/release-indexer/internal/infohash"
	"github.com/wyvernzora/kura/services/release-indexer/internal/store"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/rawpost"
)

type Store interface {
	IngestN(ctx context.Context, p store.IngestParams) (store.IngestOutcome, error)
}

type Metrics interface {
	IngestPost(source, result string)
}

type Processor struct {
	store   Store
	metrics Metrics
}

func New(s Store, metrics Metrics) *Processor {
	return &Processor{store: s, metrics: metrics}
}

// Failure identifies the post that stopped a batch.
type Failure struct {
	Index    int
	Source   string
	SourceID string
	Err      error
}

func (e *Failure) Error() string {
	return fmt.Sprintf("ingest post %d (%s/%s): %v", e.Index, e.Source, e.SourceID, e.Err)
}

func (e *Failure) Unwrap() error { return e.Err }

// Batch normalizes and ingests posts in order, stopping at the first failure.
func (p *Processor) Batch(ctx context.Context, posts []rawpost.RawPost) (rawpost.IngestBatch, error) {
	var batch rawpost.IngestBatch
	for i := range posts {
		post := posts[i]

		ih, err := infohash.NormalizeInfohash(post.Magnet)
		if err != nil {
			if errors.Is(err, infohash.ErrSkipInfohash) {
				batch.Skipped++
				p.record(post.Source, "skipped")
				continue
			}
			p.record(post.Source, "error")
			return batch, failure(i, post, err)
		}

		out, err := p.store.IngestN(ctx, params(post, ih))
		if err != nil {
			p.record(post.Source, "error")
			return batch, failure(i, post, err)
		}
		switch {
		case out.New:
			batch.New++
			p.record(post.Source, "new")
		case out.Updated:
			batch.Updated++
			p.record(post.Source, "updated")
		case out.Duplicate:
			batch.Duplicate++
			p.record(post.Source, "duplicate")
		case out.Conflict:
			batch.Conflict++
			p.record(post.Source, "conflict")
		}
	}
	return batch, nil
}

func (p *Processor) record(source, result string) {
	if p.metrics != nil {
		p.metrics.IngestPost(source, result)
	}
}

func failure(index int, post rawpost.RawPost, err error) *Failure {
	return &Failure{
		Index:    index,
		Source:   post.Source,
		SourceID: post.SourceID,
		Err:      err,
	}
}

func params(p rawpost.RawPost, infohash string) store.IngestParams {
	return store.IngestParams{
		Infohash:    infohash,
		Source:      p.Source,
		SourceID:    p.SourceID,
		Title:       p.Title,
		URL:         p.URL,
		Magnet:      p.Magnet,
		SizeBytes:   p.SizeBytes,
		PublishedAt: p.PublishedAt,
	}
}
