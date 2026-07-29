package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	relapi "github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

// crawlCmd drives the source-scoped crawl API. Each request is an exact-sized
// count+cursor chunk; --loop keeps threading the returned cursor client-side.
// Fetching, parsing, rate limiting, caching, and ingestion all stay in-process
// in the indexer.
type crawlCmd struct {
	Source   string `arg:"" required:"" enum:"dmhy,nyaa" help:"Source to crawl: dmhy or nyaa."`
	Count    int    `name:"count" default:"200" help:"Exact posts per request (1-200)."`
	Cursor   string `name:"cursor" help:"Opaque resume cursor from a previous crawl response."`
	Lookback string `name:"lookback" help:"Stop at now minus this duration (for example 30d or 2w12h); empty walks to the archive floor."`
	Loop     bool   `name:"loop" help:"Keep requesting chunks until the lookback boundary or archive floor."`
	JSON     bool   `name:"json" help:"Print raw JSON; loop mode emits one compact JSON object per chunk."`
}

func (cmd *crawlCmd) Run(rt *runContext) error {
	if cmd.Count < 1 || cmd.Count > 200 {
		return fmt.Errorf("--count must be within 1..200")
	}
	c := clientFromRT(rt)
	cursor := cmd.Cursor
	var totals crawlTotals
	for {
		resp, err := c.CrawlSource(rt.Context, cmd.Source, relapi.CrawlRequest{
			PageSize: cmd.Count,
			Cursor:   cursor,
			Lookback: cmd.Lookback,
		})
		if err != nil {
			if cursor == "" {
				return err
			}
			return fmt.Errorf("%w\n  resume with: kura crawl %s --count %d --cursor %s%s",
				err, cmd.Source, cmd.Count, cursor, lookbackArg(cmd.Lookback))
		}
		totals.add(resp)
		if err := printCrawlResult(rt, resp, cmd.JSON); err != nil {
			return err
		}
		if !cmd.Loop || !resp.HasMore {
			if cmd.Loop && !cmd.JSON {
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
		fmt.Fprintf(rt.Stdout, "  queue available %d; next cursor %s\n", resp.Queue.Available, resp.NextCursor)
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

func lookbackArg(lookback string) string {
	if lookback == "" {
		return ""
	}
	return " --lookback " + lookback
}
