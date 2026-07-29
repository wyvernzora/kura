package client

import (
	"context"
	"net/http"
	"net/url"

	tapeapi "github.com/wyvernzora/kura/services/tape-backup/pkg/api"
)

// TapeStatus calls GET /api/tape/status.
func (c *Client) TapeStatus(ctx context.Context) (tapeapi.StatusResult, error) {
	var out tapeapi.StatusResult
	err := c.Do(ctx, http.MethodGet, "/api/tape/status", nil, nil, &out)
	return out, err
}

// TapeConsult calls POST /api/tape/consult.
func (c *Client) TapeConsult(
	ctx context.Context,
	blanks []tapeapi.Blank,
) (tapeapi.ConsultResult, error) {
	var out tapeapi.ConsultResult
	err := c.Do(ctx, http.MethodPost, "/api/tape/consult", nil, blanks, &out)
	return out, err
}

// TapePlan calls POST /api/tape/plan.
func (c *Client) TapePlan(
	ctx context.Context,
	request tapeapi.PlanRequest,
) (tapeapi.PlanResult, error) {
	var out tapeapi.PlanResult
	err := c.Do(ctx, http.MethodPost, "/api/tape/plan", nil, request, &out)
	return out, err
}

// TapeRun calls POST /api/tape/run.
func (c *Client) TapeRun(
	ctx context.Context,
	request tapeapi.PlanRequest,
) (tapeapi.RunResult, error) {
	var out tapeapi.RunResult
	// A run spans the operator tape-load wait, bounded by the server's
	// IdleTimeout, plus a potentially multi-hour copy. A client timeout would
	// disconnect, cancel r.Context(), and make R18 retire and discard the plan.
	// Ctrl-C still cancels ctx and produces that clean retirement deliberately.
	err := c.WithoutTimeout().Do(
		ctx,
		http.MethodPost,
		"/api/tape/run",
		nil,
		request,
		&out,
	)
	return out, err
}

// TapeApprove calls POST /api/tape/approve/{planID}.
func (c *Client) TapeApprove(ctx context.Context, planID string) error {
	return c.Do(
		ctx,
		http.MethodPost,
		"/api/tape/approve/"+url.PathEscape(planID),
		nil,
		nil,
		nil,
	)
}

// TapeDiscard calls POST /api/tape/discard/{planID}.
func (c *Client) TapeDiscard(ctx context.Context, planID string) error {
	return c.Do(
		ctx,
		http.MethodPost,
		"/api/tape/discard/"+url.PathEscape(planID),
		nil,
		nil,
		nil,
	)
}
