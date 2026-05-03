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

	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
)

// ListInput parameters for the List workflow.
//
//   - Statuses: empty filters none; non-empty admits only those statuses.
//   - MaxResults: 0 = no limit (CLI default); >0 caps the page size.
//     Values above maxListPageSize are clamped.
//   - Cursor: empty for first page; otherwise an opaque token returned
//     by the previous page's NextCursor.
type ListInput struct {
	Statuses   []response.ListStatus
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
func List(ctx context.Context, deps Deps, in ListInput) (response.ListResult, error) {
	rows, err := deps.Index.Snapshot()
	if errors.Is(err, indexfile.ErrNotReady) {
		return response.ListResult{}, &ServerNotReadyError{Reason: "library index is rebuilding"}
	}
	if err != nil {
		return response.ListResult{}, err
	}

	if len(in.Statuses) > 0 {
		filtered := rows[:0]
		for _, row := range rows {
			if slices.Contains(in.Statuses, row.Status) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}

	pageSize := in.MaxResults
	if pageSize < 0 {
		return response.ListResult{}, fmt.Errorf("MaxResults must be >= 0")
	}
	if pageSize > maxListPageSize {
		pageSize = maxListPageSize
	}

	view := computeViewHash(rows)

	startAt := 0
	dataChanged := false
	if in.Cursor != "" {
		cursorView, anchor, err := decodeListCursor(in.Cursor)
		if err != nil {
			return response.ListResult{}, &InvalidCursorError{Reason: err.Error()}
		}
		matchesView := bytes.Equal(cursorView, view)
		idx := indexOfAnchor(rows, anchor)
		switch {
		case matchesView && idx < 0:
			return response.ListResult{}, &InvalidCursorError{Reason: "anchor not found despite matching view"}
		case matchesView:
			startAt = idx + 1
		case idx < 0:
			startAt = 0
			dataChanged = true
		default:
			startAt = idx + 1
			dataChanged = true
		}
	}

	page := rows
	if startAt > 0 {
		page = page[startAt:]
	}
	pageCap := pageSize
	if pageCap == 0 || pageCap > len(page) {
		pageCap = len(page)
	}
	page = page[:pageCap]

	out := make([]response.ListRow, 0, len(page))
	for _, row := range page {
		out = append(out, rowToListRow(row))
	}

	progress.Start(ctx, "list", "Listing library contents", len(out))
	progress.Success(ctx, "list", fmt.Sprintf("Listed library contents (%d series)", len(out)), len(out))

	result := response.ListResult{Rows: out, DataChanged: dataChanged}
	if pageSize > 0 && len(page) == pageSize && startAt+len(page) < len(rows) {
		last := page[len(page)-1]
		result.NextCursor = encodeListCursor(view, anchorHash(last.Series.String()))
	}
	return result, nil
}

func rowToListRow(row indexfile.Row) response.ListRow {
	return response.ListRow{
		Status:            row.Status,
		Staged:            row.Staged,
		Title:             row.Title,
		CanonicalTitle:    row.CanonicalTitle,
		SeasonsAvailable:  row.SeasonsAvailable,
		SeasonCount:       row.SeasonCount,
		EpisodesAvailable: row.EpisodesAvailable,
		EpisodeCount:      row.EpisodeCount,
		MetadataRef:       row.Metadata,
		Resolutions:       row.Resolutions,
		Sources:           row.Sources,
		LastScanned:       row.LastScanned,
		Error:             row.Error,
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
func decodeListCursor(cursor string) ([]byte, []byte, error) {
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(cursor)
	if err != nil {
		return nil, nil, errors.New("malformed base32 cursor")
	}
	if len(raw) != 2*hashBytes {
		return nil, nil, fmt.Errorf("cursor length %d, want %d", len(raw), 2*hashBytes)
	}
	return raw[:hashBytes], raw[hashBytes:], nil
}
