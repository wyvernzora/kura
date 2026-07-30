package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	relapi "github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

// crawlCmd drives the source-scoped crawl API. It threads bounded server-side
// chunks until the lookback boundary, printing each committed cursor as a
// durable checkpoint. Fetching, parsing, pacing, caching, and ingestion stay
// in-process in the indexer.
type crawlCmd struct {
	Source   string `arg:"" required:"" enum:"dmhy,nyaa" help:"Source to crawl: dmhy or nyaa."`
	Lookback string `arg:"" required:"" help:"Stop at now minus this duration (for example 30d or 2w12h); 0 walks to the archive floor."`
	Cursor   string `name:"cursor" help:"Opaque resume cursor from a previous crawl response."`
	JSON     bool   `name:"json" help:"Print one compact JSON object per committed chunk (JSONL)."`
}

func (cmd *crawlCmd) Run(rt *runContext) error {
	c := clientFromRT(rt)
	cursor := cmd.Cursor
	var totals crawlTotals
	for {
		resp, err := c.CrawlSource(rt.Context, cmd.Source, relapi.CrawlRequest{
			PageSize: 200,
			Cursor:   cursor,
			Lookback: cmd.Lookback,
		})
		if err != nil {
			if cursor == "" {
				return err
			}
			return fmt.Errorf("%w\n  resume with: kura crawl %s %s --cursor %s",
				err, cmd.Source, cmd.Lookback, cursor)
		}
		totals.add(resp)
		if err := printCrawlResult(rt, resp, cmd.JSON); err != nil {
			return err
		}
		if !resp.HasMore {
			if !cmd.JSON {
				fmt.Fprintf(rt.Stdout, "done: %s (%d posts, %d new, %d duplicate across %d requests)\n",
					humanStopReason(resp.StopReason), totals.posts, totals.new, totals.duplicate, totals.requests)
			}
			return nil
		}
		if resp.NextCursor == "" || resp.NextCursor == cursor {
			return errors.New("crawl server returned hasMore without a new nextCursor")
		}
		cursor = resp.NextCursor
	}
}

func printCrawlResult(rt *runContext, resp relapi.CrawlResult, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(rt.Stdout).Encode(resp)
	}
	fmt.Fprintf(rt.Stdout, "%s: %d posts from %d pages (%d new, %d updated, %d duplicate, %d conflict, %d skipped)\n",
		resp.Source, resp.Posts, resp.PagesFetched,
		resp.Batch.New, resp.Batch.Updated, resp.Batch.Duplicate, resp.Batch.Conflict, resp.Batch.Skipped)
	if resp.OldestPublishedAt != nil && resp.NewestPublishedAt != nil {
		fmt.Fprintf(rt.Stdout, "  stamps %s .. %s\n",
			resp.OldestPublishedAt.UTC().Format(time.RFC3339),
			resp.NewestPublishedAt.UTC().Format(time.RFC3339))
	}
	if resp.HasMore {
		fmt.Fprintf(rt.Stdout, "  queue available %d; checkpoint %s\n", resp.Queue.Available, resp.NextCursor)
	} else {
		fmt.Fprintf(rt.Stdout, "  queue available %d; done: %s\n", resp.Queue.Available, humanStopReason(resp.StopReason))
	}
	return nil
}

type crawlTotals struct {
	requests  int
	posts     int
	new       int
	duplicate int
}

func (t *crawlTotals) add(resp relapi.CrawlResult) {
	t.requests++
	t.posts += resp.Posts
	t.new += resp.Batch.New
	t.duplicate += resp.Batch.Duplicate
}

func humanStopReason(reason string) string {
	switch reason {
	case relapi.CrawlStopLookbackBoundary:
		return "lookback boundary"
	case relapi.CrawlStopArchiveFloor:
		return "archive floor"
	case relapi.CrawlStopPageBudget:
		return "page budget"
	default:
		return reason
	}
}
