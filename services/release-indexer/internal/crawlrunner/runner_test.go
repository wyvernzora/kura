package crawlrunner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

func TestRunnerRunsImmediatelyAndOnInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		var mu sync.Mutex
		runs := 0
		runner := Runner{
			Jobs: []Job{{
				Source:   "test",
				Interval: time.Minute,
				Timeout:  10 * time.Second,
				Crawl: func(context.Context, func([]api.RawPost) error) error {
					mu.Lock()
					runs++
					mu.Unlock()
					return nil
				},
			}},
			Ingest: func(context.Context, []api.RawPost) (api.IngestBatch, error) {
				return api.IngestBatch{}, nil
			},
		}

		done := make(chan struct{})
		go func() {
			runner.Run(ctx)
			close(done)
		}()

		synctest.Wait()
		assertRuns(t, &mu, &runs, 1)

		time.Sleep(time.Minute)
		synctest.Wait()
		assertRuns(t, &mu, &runs, 2)

		cancel()
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("Runner did not stop after cancellation")
		}
	})
}

func TestRunnerAppliesPerRunTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		timedOut := make(chan struct{}, 1)
		runner := Runner{
			Jobs: []Job{{
				Source:   "test",
				Interval: time.Hour,
				Timeout:  time.Minute,
				Crawl: func(ctx context.Context, _ func([]api.RawPost) error) error {
					<-ctx.Done()
					timedOut <- struct{}{}
					return ctx.Err()
				},
			}},
			Ingest: func(context.Context, []api.RawPost) (api.IngestBatch, error) {
				t.Fatal("Ingest called after crawl timeout")
				return api.IngestBatch{}, nil
			},
		}

		done := make(chan struct{})
		go func() {
			runner.Run(ctx)
			close(done)
		}()
		synctest.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()

		select {
		case <-timedOut:
		default:
			t.Fatal("crawl did not observe timeout")
		}

		cancel()
		synctest.Wait()
		<-done
	})
}

// Pages are ingested as they are emitted, and an ingest failure stops the
// walk without discarding the pages that already landed.
func TestRunnerIngestsPerPageAndStopsOnIngestError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		pageOne := []api.RawPost{{SourceID: "1"}, {SourceID: "2"}}
		pageTwo := []api.RawPost{{SourceID: "3"}}
		boom := errors.New("ingest down")

		var mu sync.Mutex
		var ingested [][]api.RawPost
		var crawlErr error

		runner := Runner{
			Jobs: []Job{{
				Source:   "test",
				Interval: time.Hour,
				Timeout:  time.Minute,
				Crawl: func(_ context.Context, emit func([]api.RawPost) error) error {
					if err := emit(pageOne); err != nil {
						t.Errorf("first page emit error = %v, want nil", err)
					}
					err := emit(pageTwo)
					mu.Lock()
					crawlErr = err
					mu.Unlock()
					return err
				},
			}},
			Ingest: func(_ context.Context, posts []api.RawPost) (api.IngestBatch, error) {
				mu.Lock()
				defer mu.Unlock()
				ingested = append(ingested, posts)
				if len(ingested) == 2 {
					return api.IngestBatch{}, boom
				}
				return api.IngestBatch{New: len(posts)}, nil
			},
		}

		done := make(chan struct{})
		go func() {
			runner.Run(ctx)
			close(done)
		}()
		synctest.Wait()

		mu.Lock()
		if len(ingested) != 2 || len(ingested[0]) != 2 || len(ingested[1]) != 1 {
			t.Fatalf("ingested pages = %v, want the two emitted pages in order", ingested)
		}
		if !errors.Is(crawlErr, boom) {
			t.Fatalf("emit returned %v, want the ingest error", crawlErr)
		}
		mu.Unlock()

		cancel()
		synctest.Wait()
		<-done
	})
}

func assertRuns(t *testing.T, mu *sync.Mutex, runs *int, want int) {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	if *runs != want {
		t.Fatalf("runs = %d, want %d", *runs, want)
	}
}
