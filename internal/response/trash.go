package response

import (
	"time"

	"github.com/wyvernzora/kura/internal/domain/refs"
)

// TrashList is workflow.TrashList's response. Series are sorted by
// SeriesRef; entries within each series are sorted by ULID
// (chronological order since ULIDs are time-ordered).
type TrashList struct {
	Series       []TrashSeriesEntry `json:"series"`
	TotalEntries int                `json:"totalEntries"`
	TotalBytes   int64              `json:"totalBytes"`
}

// TrashSeriesEntry rolls up trash for one series.
type TrashSeriesEntry struct {
	Ref     refs.Series  `json:"ref"`
	Entries []TrashEntry `json:"entries"`
	Bytes   int64        `json:"bytes"`
}

// TrashEntry mirrors trashfile.Meta in surface-friendly shape.
type TrashEntry struct {
	ID         string           `json:"id"`
	Episode    refs.Episode     `json:"episode"`
	TrashedAt  time.Time        `json:"trashedAt"`
	MediaPath  string           `json:"mediaPath"`
	Source     string           `json:"source,omitempty"`
	Resolution string           `json:"resolution,omitempty"`
	Size       int64            `json:"size"`
	Companions []TrashCompanion `json:"companions,omitempty"`
}

type TrashCompanion struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// TrashEmpty is workflow.TrashEmpty's response. Attempts is the total
// number of trash entries the workflow tried to delete across every
// targeted series (matched the OlderThan filter); it's >= TotalEntries
// (entries actually removed). Failures lists per-series errors that
// stopped one or more deletions; under --all they're best-effort
// (subsequent series still process), under single-series they cause
// the workflow to return an error instead. Surfaces enough signal so
// callers can distinguish "no trash matched the filter" from "every
// attempt failed" — the latter previously rendered as "Nothing to
// empty," which was misleading.
type TrashEmpty struct {
	Series         []TrashSeriesEmpty  `json:"series"`
	TotalEntries   int                 `json:"totalEntries"`
	Attempts       int                 `json:"attempts,omitempty"`
	ReclaimedBytes int64               `json:"reclaimedBytes"`
	Failures       []TrashEmptyFailure `json:"failures,omitempty"`
}

type TrashSeriesEmpty struct {
	Ref            refs.Series `json:"ref"`
	Removed        []string    `json:"removed"`
	ReclaimedBytes int64       `json:"reclaimedBytes"`
}

// TrashEmptyFailure is one series whose trash-empty attempt errored.
// Error carries the wrapped cause's Error() string; structured payload
// stays in the server log via deps.Logger.Warn.
type TrashEmptyFailure struct {
	Ref   refs.Series `json:"ref"`
	Error string      `json:"error"`
}

// TrashRestore is workflow.TrashRestore's response. Caller passed ref
// + trash entry ID; the new info is which episode slot the entry came
// from (recorded at trash time) and the list of paths that got moved
// back into place. Restored paths are series-relative slash form.
type TrashRestore struct {
	Episode  refs.Episode `json:"episode"`
	Restored []string     `json:"restored"`
}
