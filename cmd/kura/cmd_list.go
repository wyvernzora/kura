package main

import (
	clipkg "github.com/wyvernzora/kura/internal/cli"
	"github.com/wyvernzora/kura/internal/cli/client"
	"github.com/wyvernzora/kura/internal/cli/render"
	"github.com/wyvernzora/kura/internal/response"
)

type listCmd struct {
	JSON     bool     `name:"json" help:"Print machine-readable JSON instead of a human summary."`
	Statuses []string `name:"status" help:"Only show entries with this status. Repeat for multiple statuses."`
	// Airing is a tri-state filter on Row.IsAiring.
	//
	//   - omitted (kong default for *bool): no filter.
	//   - --airing or --airing=true: airing only.
	//   - --no-airing / --airing=false: non-airing only.
	Airing *bool    `name:"airing" negatable:"" help:"Filter on the airing flag (independent of status). Use --airing for currently-airing only or --no-airing for non-airing only."`
	Tags   []string `name:"tag" help:"Tag filter. Plain tags must be present; !tag expressions must be absent. Repeat for multiple expressions."`
}

// listPageSize is the per-request page cap. Server clamps to 1000;
// matching it here minimizes round trips for large libraries.
const listPageSize = 1000

// listMaxRetries bounds the cursor-walk restart count when the
// server reports DataChanged mid-walk. Pathological churn (an agent
// hammering the library while we list) shouldn't pin the CLI in a
// retry loop forever; surface the partial snapshot with the
// DataChanged flag set after the cap.
const listMaxRetries = 5

func (cmd *listCmd) Run(rt *runContext) error {
	statuses := make([]string, 0, len(cmd.Statuses))
	for _, raw := range cmd.Statuses {
		// Validate locally so a typo errors out before the round trip.
		status, err := clipkg.ParseListStatus(raw)
		if err != nil {
			return err
		}
		statuses = append(statuses, string(status))
	}
	c := clientFromRT(rt)

	var result response.ListResult
	for attempt := 0; attempt <= listMaxRetries; attempt++ {
		all, drifted, err := walkListPages(rt, c, statuses, cmd.Airing, cmd.Tags)
		if err != nil {
			return err
		}
		if !drifted {
			result = all
			break
		}
		// Restart the walk for a consistent snapshot. On the final
		// attempt, surface what we got with DataChanged=true so
		// callers know the snapshot may have ordering quirks.
		if attempt == listMaxRetries {
			all.DataChanged = true
			result = all
			break
		}
	}
	return render.List(rt.Stdout, result, cmd.JSON)
}

// walkListPages iterates the cursor chain accumulating rows.
// Returns drifted=true when any page reports DataChanged, so the
// caller can decide whether to retry for a consistent snapshot.
func walkListPages(rt *runContext, c *client.Client, statuses []string, airing *bool, tags []string) (response.ListResult, bool, error) {
	var all response.ListResult
	cursor := ""
	drifted := false
	for {
		page, err := c.ListSeries(rt.Context, statuses, airing, tags, listPageSize, cursor)
		if err != nil {
			return response.ListResult{}, false, err
		}
		if page.DataChanged {
			drifted = true
		}
		all.Rows = append(all.Rows, page.Rows...)
		if page.NextCursor == "" {
			return all, drifted, nil
		}
		cursor = page.NextCursor
	}
}
