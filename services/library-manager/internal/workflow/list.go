package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/wyvernzora/kura/services/library-manager/internal/progress"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/indexfile"
	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
)

// ListInput parameters for the List workflow.
//
//   - Statuses: empty filters none; non-empty admits only those statuses.
//   - Tags: plain expressions require presence; !tag requires absence.
//   - MaxResults: 0 = no limit (CLI default); >0 caps the page size.
//     Values above maxListPageSize are clamped.
//   - Cursor: empty for first page; otherwise an opaque token returned
//     by the previous page's NextCursor.
type ListInput struct {
	Statuses []api.ListStatus
	// Airing is a tri-state filter on Row.IsAiring. nil = no filter,
	// non-nil = admit only rows whose IsAiring matches.
	Airing     *bool
	Tags       []string
	MaxResults int
	Cursor     string
	Now        time.Time
}

const (
	// hashBytes is the per-half truncation length of the cursor hashes
	// in bytes. 8 bytes → 2^64 distinct anchors per half, plenty for a
	// single library; truncated viewHash collisions are tolerated as a
	// false-positive DataChanged signal.
	hashBytes = 8

	// maxListPageSize caps MaxResults to prevent unbounded responses.
	maxListPageSize = 1000
)

// List returns rows for tracked + untracked entries from the in-memory
// index, optionally filtered and paginated. The disk is not walked.
//
// Returns ServerNotReadyError when the index is rebuilding from a cold
// start (or corruption recovery) and has nothing to serve yet.
func List(ctx context.Context, deps Deps, in ListInput) (api.ListResult, error) {
	rows, err := deps.Index.Snapshot()
	if errors.Is(err, indexfile.ErrNotReady) {
		return api.ListResult{}, &ServerNotReadyError{Reason: "library index is rebuilding"}
	}
	if err != nil {
		return api.ListResult{}, err
	}

	rows = applyStatusFilter(rows, in.Statuses)
	rows = applyAiringFilter(rows, in.Airing)
	tagFilter, err := parseTagExpressions(in.Tags, true)
	if err != nil {
		return api.ListResult{}, err
	}
	rows = applyTagFilter(rows, tagFilter)

	pageSize, err := normalizePageSize(in.MaxResults)
	if err != nil {
		return api.ListResult{}, err
	}

	view := computeViewHash(rows)
	startAt, dataChanged, err := resolveCursorPosition(rows, in.Cursor, view)
	if err != nil {
		return api.ListResult{}, err
	}

	page := paginateRows(rows, startAt, pageSize)
	out := make([]api.ListRow, 0, len(page))
	for _, row := range page {
		out = append(out, rowToListRow(row))
	}

	progress.Start(ctx, "list", "Listing library contents", len(out))
	progress.Success(ctx, "list", fmt.Sprintf("Listed library contents (%d series)", len(out)), len(out))

	return api.ListResult{
		Rows:        out,
		DataChanged: dataChanged,
		NextCursor:  nextListCursor(view, page, pageSize, startAt, len(rows)),
	}, nil
}

// applyStatusFilter narrows rows to entries whose Status is in the
// allow-list. Empty statuses returns rows unchanged.
func applyStatusFilter(rows []indexfile.Row, statuses []api.ListStatus) []indexfile.Row {
	if len(statuses) == 0 {
		return rows
	}
	filtered := rows[:0]
	for _, row := range rows {
		if slices.Contains(statuses, row.Status) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// applyAiringFilter narrows rows by Row.IsAiring when airing is non-nil.
// Nil passes rows through unchanged.
func applyAiringFilter(rows []indexfile.Row, airing *bool) []indexfile.Row {
	if airing == nil {
		return rows
	}
	want := *airing
	filtered := rows[:0]
	for _, row := range rows {
		if row.IsAiring == want {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// normalizePageSize validates MaxResults and clamps to maxListPageSize.
// Negative values are rejected; 0 is "no limit" and passes through.
func normalizePageSize(maxResults int) (int, error) {
	if maxResults < 0 {
		return 0, fmt.Errorf("MaxResults must be >= 0")
	}
	if maxResults > maxListPageSize {
		return maxListPageSize, nil
	}
	return maxResults, nil
}

// resolveCursorPosition decodes the opaque cursor against the current
// rows + view. Returns the row index to start the page at, whether the
// underlying view diverged from the cursor's recorded view, and any
// decode / lookup error. Empty cursor is the first-page case (startAt
// 0, dataChanged false).
func resolveCursorPosition(rows []indexfile.Row, cursor string, view []byte) (startAt int, dataChanged bool, err error) {
	if cursor == "" {
		return 0, false, nil
	}
	cursorView, anchor, decodeErr := decodeListCursor(cursor)
	if decodeErr != nil {
		return 0, false, &InvalidCursorError{Reason: decodeErr.Error()}
	}
	matchesView := bytes.Equal(cursorView, view)
	idx := indexOfAnchor(rows, anchor)
	switch {
	case matchesView && idx < 0:
		return 0, false, &InvalidCursorError{Reason: "anchor not found despite matching view"}
	case matchesView:
		return idx + 1, false, nil
	case idx < 0:
		return 0, true, nil
	default:
		return idx + 1, true, nil
	}
}

// paginateRows slices rows by startAt and pageSize. pageSize 0 (or
// larger than the remaining tail) returns the full tail.
func paginateRows(rows []indexfile.Row, startAt, pageSize int) []indexfile.Row {
	page := rows
	if startAt > 0 {
		page = page[startAt:]
	}
	pageCap := pageSize
	if pageCap == 0 || pageCap > len(page) {
		pageCap = len(page)
	}
	return page[:pageCap]
}

// nextListCursor returns the encoded cursor pointing past the current
// page, or empty when there are no more rows or pagination is disabled.
func nextListCursor(view []byte, page []indexfile.Row, pageSize, startAt, total int) string {
	if pageSize == 0 || len(page) != pageSize || startAt+len(page) >= total {
		return ""
	}
	last := page[len(page)-1]
	return encodeListCursor(view, anchorHash(last.Series.String()))
}

func rowToListRow(row indexfile.Row) api.ListRow {
	return api.ListRow{
		Status:             row.Status,
		IsAiring:           row.IsAiring,
		Staged:             row.Staged,
		Title:              row.Title,
		CanonicalTitle:     row.CanonicalTitle,
		SeasonsAvailable:   row.SeasonsAvailable,
		SeasonCount:        row.SeasonCount,
		EpisodesAvailable:  row.EpisodesAvailable,
		EpisodeCount:       row.EpisodeCount,
		MetadataRef:        row.Metadata,
		Resolutions:        row.Resolutions,
		Sources:            row.Sources,
		Tags:               row.Tags,
		PosterURL:          row.PosterURL,
		PosterThumbnailURL: row.PosterThumbnailURL,
		DateAdded:          row.DateAdded,
		LastAired:          row.LastAired,
		LastScanned:        row.LastScanned,
		SearchKey:          row.SearchKey,
		Error:              row.Error,
	}
}

// computeViewHash returns the first hashBytes bytes of SHA-256 over the
// joined series-ref strings of rows, separated by '\n'. Empty input yields
// an all-zero result.
func computeViewHash(rows []indexfile.Row) []byte {
	if len(rows) == 0 {
		return make([]byte, hashBytes)
	}
	var b strings.Builder
	for i, row := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(row.Series.String())
	}
	sum := sha256.Sum256([]byte(b.String()))
	return sum[:hashBytes]
}

// anchorHash returns the first hashBytes bytes of SHA-256 over the
// raw series-ref string. Used for cursor anchor and for resume-by-anchor
// scans.
func anchorHash(seriesRef string) []byte {
	sum := sha256.Sum256([]byte(seriesRef))
	return sum[:hashBytes]
}

// indexOfAnchor returns the index of the row whose series ref hashes to
// anchor, or -1 if none match.
func indexOfAnchor(rows []indexfile.Row, anchor []byte) int {
	for i, row := range rows {
		if bytes.Equal(anchorHash(row.Series.String()), anchor) {
			return i
		}
	}
	return -1
}

// encodeListCursor packs viewHash and anchorHash side-by-side without
// a separator; length is fixed (2*hashBytes) so split is by offset.
// base32 (RFC 4648, no padding) keeps the output case-insensitive and
// safe to copy out of MCP logs.
func encodeListCursor(viewHash, anchor []byte) string {
	if len(viewHash) != hashBytes || len(anchor) != hashBytes {
		return ""
	}
	buf := make([]byte, 2*hashBytes)
	copy(buf[:hashBytes], viewHash)
	copy(buf[hashBytes:], anchor)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
}

// decodeListCursor reverses encodeListCursor. Returns view, anchor, err.
// Bad length, bad base32, or unexpected payload size yields an error.
func decodeListCursor(cursor string) (view, anchor []byte, err error) {
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(cursor)
	if err != nil {
		return nil, nil, errors.New("malformed base32 cursor")
	}
	if len(raw) != 2*hashBytes {
		return nil, nil, fmt.Errorf("cursor length %d, want %d", len(raw), 2*hashBytes)
	}
	return raw[:hashBytes], raw[hashBytes:], nil
}
