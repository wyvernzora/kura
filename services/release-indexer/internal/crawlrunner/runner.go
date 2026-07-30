// Package crawlrunner schedules time-bounded source crawls and ingests their
// pages as they arrive.
package crawlrunner

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

// CrawlFunc walks a source's listing and invokes emit once per parsed page,
// newest page first. Emitting a page persists it: a later fetch or parse
// failure must not undo pages already emitted.
type CrawlFunc func(ctx context.Context, emit func([]api.RawPost) error) error

type IngestFunc func(ctx context.Context, posts []api.RawPost) (api.IngestBatch, error)

type Metrics interface {
	IngestBatch(size int, result string)
	SourceCrawl(source, result string, posts int, duration time.Duration)
	SourceCrawlSuccess(source string)
}

type Job struct {
	Source   string
	Interval time.Duration
	Timeout  time.Duration
	Crawl    CrawlFunc
}

type Runner struct {
	Jobs    []Job
	Ingest  IngestFunc
	Metrics Metrics
	Logger  *slog.Logger
}

// Run starts one owned loop per source and blocks until all loops stop.
func (r Runner) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, job := range r.Jobs {
		wg.Go(func() {
			r.runJob(ctx, job)
		})
	}
	wg.Wait()
}

func (r Runner) runJob(ctx context.Context, job Job) {
	r.runOnce(ctx, job)

	ticker := time.NewTicker(job.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runOnce(ctx, job)
		}
	}
}

func (r Runner) runOnce(ctx context.Context, job Job) {
	if ctx.Err() != nil {
		return
	}

	start := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, job.Timeout)
	defer cancel()

	var (
		pages     int
		posts     int
		batch     api.IngestBatch
		ingestErr error
	)
	crawlErr := job.Crawl(runCtx, func(pagePosts []api.RawPost) error {
		pages++
		posts += len(pagePosts)
		// Ingest per page, immediately: pages already ingested survive any
		// later failure, so a long catch-up crawl makes durable progress
		// even when it does not finish.
		pageBatch, err := r.Ingest(runCtx, pagePosts)
		addBatch(&batch, pageBatch)
		if r.Metrics != nil {
			result := "ok"
			if err != nil {
				result = "error"
			}
			r.Metrics.IngestBatch(len(pagePosts), result)
		}
		if err != nil {
			ingestErr = err
		}
		return err
	})

	if crawlErr != nil {
		result := "crawl_error"
		level := slog.LevelWarn
		message := "scheduled crawl failed"
		if ingestErr != nil {
			result = "ingest_error"
			level = slog.LevelError
			message = "scheduled crawl ingest failed"
		}
		r.record(job.Source, result, posts, time.Since(start))
		r.log(runCtx, level, message,
			"source", job.Source,
			"page_count", pages,
			"post_count", posts,
			"new_count", batch.New,
			"duration_ms", time.Since(start).Milliseconds(),
			"err", crawlErr,
		)
		return
	}

	r.record(job.Source, "ok", posts, time.Since(start))
	if r.Metrics != nil {
		// Per-page IngestBatch is emitted inside the emit closure; the
		// run-level success marker still fires once per clean run.
		r.Metrics.SourceCrawlSuccess(job.Source)
	}
	r.log(runCtx, slog.LevelInfo, "scheduled crawl completed",
		"source", job.Source,
		"page_count", pages,
		"post_count", posts,
		"new_count", batch.New,
		"updated_count", batch.Updated,
		"duplicate_count", batch.Duplicate,
		"conflict_count", batch.Conflict,
		"skipped_count", batch.Skipped,
		"duration_ms", time.Since(start).Milliseconds(),
	)
}

func addBatch(dst *api.IngestBatch, b api.IngestBatch) {
	dst.New += b.New
	dst.Updated += b.Updated
	dst.Duplicate += b.Duplicate
	dst.Conflict += b.Conflict
	dst.Skipped += b.Skipped
}

func (r Runner) record(source, result string, posts int, duration time.Duration) {
	if r.Metrics != nil {
		r.Metrics.SourceCrawl(source, result, posts, duration)
	}
}

func (r Runner) log(ctx context.Context, level slog.Level, message string, attrs ...any) {
	if r.Logger != nil {
		r.Logger.Log(ctx, level, message, attrs...)
	}
}
