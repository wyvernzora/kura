package crawlrunner

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/rawpost"
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
				Crawl: func(context.Context) ([]rawpost.RawPost, error) {
					mu.Lock()
					runs++
					mu.Unlock()
					return nil, nil
				},
			}},
			Ingest: func(context.Context, []rawpost.RawPost) (rawpost.IngestBatch, error) {
				return rawpost.IngestBatch{}, nil
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
				Crawl: func(ctx context.Context) ([]rawpost.RawPost, error) {
					<-ctx.Done()
					timedOut <- struct{}{}
					return nil, ctx.Err()
				},
			}},
			Ingest: func(context.Context, []rawpost.RawPost) (rawpost.IngestBatch, error) {
				t.Fatal("Ingest called after crawl timeout")
				return rawpost.IngestBatch{}, nil
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

func assertRuns(t *testing.T, mu *sync.Mutex, runs *int, want int) {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	if *runs != want {
		t.Fatalf("runs = %d, want %d", *runs, want)
	}
}
