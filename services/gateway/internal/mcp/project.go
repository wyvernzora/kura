package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/wyvernzora/kura/services/gateway/internal/client"
)

// The projections in this file are the only places a leaf response is reshaped
// rather than forwarded. Everything else reaches structuredContent unchanged.
//
// They operate on map[string]any rather than typed structs on purpose: a
// partial struct would silently drop any field a leaf added, and these
// projections remove named fields rather than selecting a fixed set.

const jobStateFailed = "failed"

// resetSummary is the compact form of a reset. The REST result carries the
// full media record for every cleared slot, which an agent that just asked to
// clear them does not need.
type resetSummary struct {
	Cleared      int      `json:"cleared"`
	TrashRemoved []string `json:"trashRemoved"`
	ExtraRemoved []string `json:"extraRemoved"`
}

func projectReset(in client.ResetResult) resetSummary {
	cleared := len(in.Records)
	if len(in.Record) > 0 {
		// A single-episode reset answers with `record`, not `records`.
		cleared++
	}
	return resetSummary{
		Cleared:      cleared,
		TrashRemoved: nonNil(in.TrashRemoved),
		ExtraRemoved: nonNil(in.ExtraRemoved),
	}
}

// projectJob applies the get_job rule: state, progress, and error always
// survive; result is dropped on success and kept on failure.
//
// On success the agent reads the outcome through get_series or list_releases,
// and a full scan or reconcile result would otherwise land in model context on
// every poll. On failure it is the opposite: a semi-autonomous agent needs the
// partial outcome to see how far the job got and correct its inputs, so
// stripping it would force the agent to give up or re-run blind.
func projectJob(in client.JobStatus) (map[string]any, error) {
	out := map[string]any{
		"jobId": in.JobID,
		"kind":  in.Kind,
		"state": in.State,
	}
	if in.Ref != "" {
		out["ref"] = in.Ref
	}
	if err := putRaw(out, "progress", in.Progress); err != nil {
		return nil, err
	}
	if err := putRaw(out, "error", in.Error); err != nil {
		return nil, err
	}
	if in.State == jobStateFailed && len(in.Result) > 0 {
		if len(in.Result) > responseBudget {
			out["result"] = map[string]any{
				"omitted":     "result exceeded the response budget",
				"budgetBytes": responseBudget,
				"actualBytes": len(in.Result),
			}
			out["resultTruncated"] = true
		} else if err := putRaw(out, "result", in.Result); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// projectSeries trims the REST series detail to the agent-facing view. The
// host-side fields go: an agent addresses series by ref and cannot use a path,
// and the generation counter is internal bookkeeping.
func projectSeries(full map[string]any) (map[string]any, error) {
	for _, hostSide := range []string{"directory", "root", "generation"} {
		delete(full, hostSide)
	}
	size, err := encodedSize(full)
	if err != nil {
		return nil, err
	}
	if size > responseBudget {
		// Truncating a structured document would produce invalid JSON,
		// so the agent is told to narrow instead. `episodes` accepts the
		// truncatedRanges a successful response already carries.
		return nil, &client.Error{
			Kind:    "response_too_large",
			Message: "series detail exceeds the response budget; narrow it with the episodes selector",
			Data:    map[string]any{"budgetBytes": responseBudget, "actualBytes": size},
		}
	}
	return full, nil
}

// projectPlan forwards the reconcile plan minus internal trash-bucket detail.
// The REST plan is already selector-safe, so this is field selection rather
// than path sanitization.
func projectPlan(full map[string]any) (map[string]any, error) {
	delete(full, "trashBucket")
	return full, nil
}

func putRaw(out map[string]any, key string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("project job %s: %w", key, err)
	}
	out[key] = v
	return nil
}

func encodedSize(v any) (int, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return 0, fmt.Errorf("measure response: %w", err)
	}
	return len(raw), nil
}

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
