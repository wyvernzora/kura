package response

// ScanAllResult is the terminal payload of POST /api/v1/library/scan.
// Aggregates the outcome of fanning per-series scans across the
// tracked library — total dispatched, succeeded, failed, and the
// per-series failure detail for any that didn't succeed. Per-series
// success detail (synced/skipped) is intentionally not aggregated; a
// caller that needs it issues a single-series scan.
type ScanAllResult struct {
	Total     int              `json:"total"`
	Succeeded int              `json:"succeeded"`
	Failed    int              `json:"failed"`
	Failures  []ScanAllFailure `json:"failures,omitempty"`
}

// ScanAllFailure carries enough context for a UI or operator to
// understand which series tripped and why. Kind is the typed error
// category from internal/errkind so consumers can branch without
// parsing free-form messages.
type ScanAllFailure struct {
	Ref     string `json:"ref"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}
