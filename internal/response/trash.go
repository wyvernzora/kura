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

// TrashEmpty is workflow.TrashEmpty's response.
type TrashEmpty struct {
	Series         []TrashSeriesEmpty `json:"series"`
	TotalEntries   int                `json:"totalEntries"`
	ReclaimedBytes int64              `json:"reclaimedBytes"`
}

type TrashSeriesEmpty struct {
	Ref            refs.Series `json:"ref"`
	Removed        []string    `json:"removed"`
	ReclaimedBytes int64       `json:"reclaimedBytes"`
}

// TrashRestore is workflow.TrashRestore's response. Caller passed ref
// + trash entry ID; the new info is which episode slot the entry came
// from (recorded at trash time) and the list of paths that got moved
// back into place.
type TrashRestore struct {
	Episode  refs.Episode `json:"episode"`
	Restored []string     `json:"restored"`
}
