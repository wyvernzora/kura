// Package crawlrunner schedules bounded source crawls and ingests their posts.
package crawlrunner

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/rawpost"
)

type CrawlFunc func(ctx context.Context) ([]rawpost.RawPost, error)

type IngestFunc func(ctx context.Context, posts []rawpost.RawPost) (rawpost.IngestBatch, error)

type Metrics interface {
	IngestBatch(size int, result string)
	SourceCrawl(source, result string, posts int, duration time.Duration)
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

	posts, err := job.Crawl(runCtx)
	if err != nil {
		r.record(job.Source, "crawl_error", 0, time.Since(start))
		r.log(runCtx, slog.LevelWarn, "scheduled crawl failed",
			"source", job.Source,
			"duration_ms", time.Since(start).Milliseconds(),
			"err", err,
		)
		return
	}

	batch, err := r.Ingest(runCtx, posts)
	if err != nil {
		r.record(job.Source, "ingest_error", len(posts), time.Since(start))
		if r.Metrics != nil {
			r.Metrics.IngestBatch(len(posts), "error")
		}
		r.log(runCtx, slog.LevelError, "scheduled crawl ingest failed",
			"source", job.Source,
			"post_count", len(posts),
			"duration_ms", time.Since(start).Milliseconds(),
			"err", err,
		)
		return
	}

	r.record(job.Source, "ok", len(posts), time.Since(start))
	if r.Metrics != nil {
		r.Metrics.IngestBatch(len(posts), "ok")
	}
	r.log(runCtx, slog.LevelInfo, "scheduled crawl completed",
		"source", job.Source,
		"post_count", len(posts),
		"new_count", batch.New,
		"updated_count", batch.Updated,
		"duplicate_count", batch.Duplicate,
		"conflict_count", batch.Conflict,
		"skipped_count", batch.Skipped,
		"duration_ms", time.Since(start).Milliseconds(),
	)
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
