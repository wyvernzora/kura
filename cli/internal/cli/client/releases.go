package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	relapi "github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

// CrawlSource asks the release indexer to consume one exact-sized chunk from
// a source cursor and ingest it directly. The caller owns the loop and threads
// NextCursor into the next request while HasMore is true.
//
// The releases surface lives on the release indexer, so KURA_SERVER_URL must
// point at the suite gateway (one origin over every service API) or at the
// indexer directly; a library-manager-only endpoint will 404.
func (c *Client) CrawlSource(ctx context.Context, source string, reqBody relapi.CrawlRequest) (relapi.CrawlResult, error) {
	var result relapi.CrawlResult
	path := "/api/v1/sources/" + url.PathEscape(source) + "/crawl"
	req, err := buildHTTPRequest(ctx, http.MethodPost, c.BaseURL+path, reqBody)
	if err != nil {
		return result, err
	}
	setRequestHeaders(req, c)

	// A count-bounded chunk is not necessarily time-bounded: old/sparse DMHY
	// history can require many individually bounded page fetches. Let the
	// request context and the indexer's per-page request_timeout own
	// cancellation instead of the generic client's five-minute wall clock.
	httpClient := http.DefaultClient
	if c.HTTPClient != nil {
		clone := *c.HTTPClient
		clone.Timeout = 0
		httpClient = &clone
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return result, releaseDiscoveryHint(err, c.BaseURL)
	}
	defer resp.Body.Close()
	if err := decodeHTTPResponse(resp, &result); err != nil {
		return result, fmt.Errorf("crawl %s: %w", source, err)
	}
	return result, nil
}

func releaseDiscoveryHint(err error, baseURL string) error {
	if _, ok := errors.AsType[*url.Error](err); ok {
		return fmt.Errorf("cannot reach release indexer at %s\n  hint: start the suite gateway or release indexer, or set %s=<url>\n  underlying: %w",
			baseURL, EnvBaseURL, err)
	}
	return err
}
