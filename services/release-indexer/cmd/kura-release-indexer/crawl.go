package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/wyvernzora/kura/services/release-indexer/internal/config"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
	"github.com/wyvernzora/kura/services/release-indexer/sources/dmhy"
	"github.com/wyvernzora/kura/services/release-indexer/sources/nyaa"
)

// runCrawlCommand implements `kura-release-indexer crawl`: fetch ONE listing
// page for one source and print its posts as ingest-ready JSONL on stdout —
// each line is exactly the element shape POST /api/v1/releases/ingest accepts
// in posts[]. The resume cursor goes to stderr so stdout stays pipeline-pure.
//
// One page per invocation is deliberate: the operator's shell loop is the
// retry unit, the resume point, and the politeness pacing (sleep between
// invocations). Backfill orchestration lives in scripts, not in the service —
// see operations.md for the recipe.
func runCrawlCommand(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("crawl", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", config.DefaultPath, "TOML configuration file")
	source := fs.String("source", "", "source to crawl: dmhy or nyaa")
	page := fs.Int("page", 1, "1-based listing page to fetch")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("crawl: unexpected positional arguments: %v", fs.Args())
	}
	if *page < 1 {
		return fmt.Errorf("crawl: -page must be >= 1")
	}

	// LoadCrawlTool needs no database URL and permits crawling a
	// configured-but-disabled source: this is an operator command.
	cfg, err := config.LoadCrawlTool(*configPath)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var posts []api.RawPost
	switch *source {
	case api.SourceDMHY:
		s := cfg.Sources.DMHY
		category, err := strconv.Atoi(s.Category)
		if err != nil {
			return fmt.Errorf("crawl: parse DMHY category: %w", err)
		}
		// No page cache: a one-shot process never refetches a page.
		crawler := dmhy.NewHTTPCrawler(s.URL, category, s.MaxRPS, 0, s.RequestTimeout)
		posts, err = crawler.Page(ctx, *page)
		if err != nil {
			return err
		}
	case api.SourceNyaa:
		s := cfg.Sources.Nyaa
		crawler := nyaa.NewHTTPCrawler(s.URL, s.Query, s.Category, s.Filter, s.MaxRPS, s.RequestTimeout)
		posts, err = crawler.Page(ctx, *page)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("crawl: -source must be %q or %q", api.SourceDMHY, api.SourceNyaa)
	}

	if len(posts) == 0 {
		fmt.Fprintln(stderr, "empty page: archive floor or past the end of the listing")
		return nil
	}

	enc := json.NewEncoder(stdout)
	for _, post := range posts {
		if err := enc.Encode(post); err != nil {
			return fmt.Errorf("crawl: encode post: %w", err)
		}
	}
	fmt.Fprintf(stderr, "next_page=%d\n", *page+1)
	return nil
}
