package api_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

// update rewrites the golden files instead of comparing against them.
// Run with: go test ./pkg/api -update
var update = flag.Bool("update", false, "rewrite golden files")

func ptr[T any](v T) *T { return &v }

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t
}

// The goldens are the frozen wire contract. They are what the REST handlers are
// held to once they mount the final routes, and what the gateway's decode
// structs are round-tripped against — a field renamed here without renaming it
// in a handler shows up as a diff in both places rather than at runtime.
//
// Coverage is one normal case per response, plus the two shapes that are easy
// to get wrong: an empty collection (which must serialize as [] and not null)
// and a fully-null nullable set (which must stay explicit null and not
// disappear or coerce to zero).
func cases() map[string]any {
	return map[string]any{
		"release_list": api.ReleaseList{
			Items: []api.ReleaseItem{{
				Infohash:    "0123456789abcdef0123456789abcdef01234567",
				Ref:         "tvdb:370070",
				Title:       "[Group] Example - 01 (WebRip 1080p)",
				SizeBytes:   ptr(int64(1503238553)),
				PublishedAt: ts("2026-07-27T12:34:56.123456Z"),
				Confidence:  ptr(0.98),
				Sources:     []string{"dmhy"},
				MatchStatus: api.MatchStatusMatched,
			}},
			NextCursor: ptr("b3BhcXVl"),
		},
		"release_list_empty": api.ReleaseList{
			Items: []api.ReleaseItem{},
		},
		"release_list_null_facts": api.ReleaseList{
			Items: []api.ReleaseItem{{
				Infohash:    "0123456789abcdef0123456789abcdef01234567",
				Ref:         "tvdb:370070",
				Title:       "[Group] Example - 01",
				SizeBytes:   nil,
				PublishedAt: ts("2026-07-27T12:34:56.123456Z"),
				Confidence:  nil,
				Sources:     []string{},
				MatchStatus: api.MatchStatusMatched,
			}},
		},
		// The attention surface's row: a release the list now returns because
		// it asked for a non-matched status, carrying no ref and no score.
		"release_list_exhausted": api.ReleaseList{
			Items: []api.ReleaseItem{{
				Infohash:    "fedcba9876543210fedcba9876543210fedcba98",
				Ref:         "",
				Title:       "[Group] Unknown - 01",
				SizeBytes:   ptr(int64(1503238553)),
				PublishedAt: ts("2026-07-27T12:34:56.123456Z"),
				Confidence:  nil,
				Sources:     []string{"nyaa"},
				MatchStatus: api.MatchStatusExhausted,
			}},
		},
		"release_detail": api.ReleaseDetail{
			Infohash:       "0123456789abcdef0123456789abcdef01234567",
			Title:          "[Group] Example - 01 (WebRip 1080p)",
			Magnet:         ptr("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"),
			SizeBytes:      ptr(int64(1503238553)),
			PublishedAt:    ts("2026-07-27T12:34:56.123456Z"),
			Sources:        []string{"dmhy", "nyaa"},
			MatchStatus:    api.MatchStatusMatched,
			Ref:            ptr("tvdb:370070"),
			Confidence:     ptr(0.98),
			FirstMatchedAt: ptr(ts("2026-07-27T13:00:00Z")),
			AttemptCount:   1,
			CreatedAt:      ts("2026-07-27T12:35:00Z"),
			UpdatedAt:      ts("2026-07-27T13:00:00Z"),
			RawItems: []api.RawItem{{
				ID:          42,
				Source:      "dmhy",
				SourceID:    "678901",
				Title:       "[Group] Example - 01 (WebRip 1080p)",
				URL:         ptr("https://share.dmhy.org/topics/view/678901.html"),
				PublishedAt: ts("2026-07-27T12:34:56.123456Z"),
				IngestedAt:  ts("2026-07-27T12:35:00Z"),
			}},
			MatchEvents: []api.MatchEvent{{
				ID:         7,
				Status:     api.MatchStatusMatched,
				Ref:        ptr("tvdb:370070"),
				Confidence: ptr(0.98),
				Reason:     nil,
				CreatedAt:  ts("2026-07-27T13:00:00Z"),
			}},
		},
		"release_detail_unmatched": api.ReleaseDetail{
			Infohash:       "fedcba9876543210fedcba9876543210fedcba98",
			Title:          "[Group] Unknown - 01",
			Magnet:         nil,
			SizeBytes:      nil,
			PublishedAt:    ts("2026-07-27T12:34:56.123456Z"),
			Sources:        []string{"nyaa"},
			MatchStatus:    api.MatchStatusUnmatched,
			Ref:            nil,
			Confidence:     nil,
			FirstMatchedAt: nil,
			AttemptCount:   0,
			CreatedAt:      ts("2026-07-27T12:35:00Z"),
			UpdatedAt:      ts("2026-07-27T12:35:00Z"),
			RawItems:       []api.RawItem{},
			MatchEvents:    []api.MatchEvent{},
		},
		"magnet": api.Magnet{
			Infohash: "0123456789abcdef0123456789abcdef01234567",
			Magnet:   "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
		},
		"claim": api.ClaimResponse{
			Items: []api.ClaimItem{{
				Infohash:       "0123456789abcdef0123456789abcdef01234567",
				ClaimToken:     1723,
				AttemptCount:   1,
				LeaseExpiresAt: ts("2026-07-27T13:05:00Z"),
				RawItems: []api.ClaimRawItem{{
					ID:          42,
					Source:      "dmhy",
					SourceID:    "678901",
					Title:       "[Group] Example - 01 (WebRip 1080p)",
					URL:         ptr("https://share.dmhy.org/topics/view/678901.html"),
					PublishedAt: ts("2026-07-27T12:34:56.123456Z"),
				}, {
					ID:          43,
					Source:      "nyaa",
					SourceID:    "1234567",
					Title:       "[Group] Example - 01",
					URL:         nil,
					PublishedAt: ts("2026-07-27T12:36:00Z"),
				}},
			}},
		},
		"claim_empty": api.ClaimResponse{Items: []api.ClaimItem{}},
		"submit":      api.SubmitResponse{Ok: true},
		"healthz":     api.Health{Ok: true, Version: "0.6.1"},
		"ingest_request": api.IngestRequest{Posts: []api.RawPost{{
			Title:       "[Group] Example - 01 (WebRip 1080p)",
			Magnet:      "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
			Source:      "dmhy",
			SourceID:    "678901",
			URL:         "https://share.dmhy.org/topics/view/678901.html",
			PublishedAt: ts("2026-07-27T12:34:56.123456Z"),
			SizeBytes:   1503238553,
		}}},
		"claim_request": api.ClaimRequest{Limit: 10, LeaseSeconds: 300},
		"submit_request": api.SubmitRequest{
			Infohash:   "0123456789abcdef0123456789abcdef01234567",
			ClaimToken: 1723,
			Status:     api.SubmitStatusMatched,
			Ref:        "tvdb:370070",
			Confidence: ptr(0.98),
		},
		"queue_stats": api.QueueStats{
			Available: 12, Leased: 2, Unmatched: 14, Matched: 900, Suppressed: 3, Exhausted: 1,
		},
		"ingest_summary": api.IngestSummary{
			Batch: api.IngestBatch{New: 3, Updated: 1, Duplicate: 12, Conflict: 0, Skipped: 2},
			Queue: api.QueueCounts{Available: 15, Leased: 2, Exhausted: 1},
		},
		"error_not_found": api.Error{
			Kind:    api.KindNotFound,
			Message: "no release with that infohash",
			Data:    map[string]any{"infohash": "0123456789abcdef0123456789abcdef01234567"},
		},
		"error_invalid_request": api.Error{
			Kind:    api.KindInvalidRequest,
			Message: "limit must be a non-negative integer",
		},
		"error_stale_lease": api.Error{
			Kind:    api.KindStaleLease,
			Message: "claim token does not match the active lease",
			Data: map[string]any{
				"infohash":   "0123456789abcdef0123456789abcdef01234567",
				"claimToken": 1723,
			},
		},
		"error_method_not_allowed": api.Error{
			Kind: api.KindMethodNotAllowed, Message: "method not allowed",
			Data: map[string]any{"allowed": []string{"GET"}},
		},
		"error_invalid_ref": api.Error{
			Kind: api.KindInvalidRef, Message: "ref must be namespace:value",
			Data: map[string]any{"ref": "not-a-ref"},
		},
		"error_invalid_cursor": api.Error{
			Kind: api.KindInvalidCursor, Message: "cursor does not decode",
			Data: map[string]any{"reason": "malformed base64"},
		},
		"error_batch_too_large": api.Error{
			Kind: api.KindBatchTooLarge, Message: "too many posts in one batch",
			Data: map[string]any{"limit": 1500, "max": 1000},
		},
		"error_no_active_lease": api.Error{
			Kind: api.KindNoActiveLease, Message: "release has no active lease",
			Data: map[string]any{"infohash": "0123456789abcdef0123456789abcdef01234567"},
		},
		"error_backend_unavailable": api.Error{
			Kind: api.KindBackendUnavailable, Message: "release index is unreachable",
			Data: map[string]any{"component": "releaseIndexer"},
		},
		"error_server_not_ready": api.Error{
			Kind: api.KindServerNotReady, Message: "service is still starting",
			Data: map[string]any{"reason": "migrations in progress"},
		},
		"error_internal": api.Error{Kind: api.KindInternal, Message: "internal error"},
		"set_status_request": api.SetStatusRequest{
			Status: api.MatchStatusDead,
			Reason: "stalled at 0% for 14 days",
		},
		// The hand-match body: `ref` is present only for a transition into
		// matched, and omitted everywhere else.
		"set_status_request_match": api.SetStatusRequest{
			Status: api.MatchStatusMatched,
			Ref:    "tvdb:370070",
			Reason: "matched by hand from the release attention list",
		},
		"error_not_matched": api.Error{
			Kind: api.KindNotMatched, Message: "release is dead, not matched",
			Data: map[string]any{
				"infohash":    "0123456789abcdef0123456789abcdef01234567",
				"matchStatus": "dead",
			},
		},
		"error_invalid_transition": api.Error{
			Kind: api.KindInvalidTransition, Message: "unmatched -> dead is not an allowed transition",
			Data: map[string]any{"infohash": "0123456789abcdef0123456789abcdef01234567"},
		},
	}
}

func TestGoldens(t *testing.T) {
	for name, value := range cases() {
		t.Run(name, func(t *testing.T) {
			got, err := json.MarshalIndent(value, "", "  ")
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got = append(got, '\n')

			path := filepath.Join("testdata", name+".json")
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatalf("mkdir testdata: %v", err)
				}
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run `go test ./pkg/api -update` to create): %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
			}
		})
	}
}

// TestGoldensRoundTrip proves marshal/unmarshal symmetry: no omitempty
// asymmetry, no type coercion, and — via DisallowUnknownFields — no key in a
// golden that the struct does not declare.
//
// It cannot catch a field the struct forgot, because TestGoldens generates
// these goldens from these same structs. That check only bites for
// hand-written decode structs held against goldens they did not produce, which
// is the gateway's internal/client in a later phase.
func TestGoldensRoundTrip(t *testing.T) {
	for name, value := range cases() {
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", name+".json"))
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			// A fresh zero value of the same type, so this test cannot drift
			// out of step with cases().
			target := reflect.New(reflect.TypeOf(value)).Interface()
			dec := json.NewDecoder(bytes.NewReader(want))
			dec.DisallowUnknownFields()
			if err := dec.Decode(target); err != nil {
				t.Fatalf("decode golden into %T: %v", target, err)
			}
			got, err := json.MarshalIndent(reflect.ValueOf(target).Elem().Interface(), "", "  ")
			if err != nil {
				t.Fatalf("re-encode: %v", err)
			}
			got = append(got, '\n')
			if !bytes.Equal(got, want) {
				t.Errorf("round trip changed %s\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
			}
		})
	}
}

// TestNoOrphanGoldens fails when testdata holds a file no case produces, which
// otherwise happens silently whenever a case is renamed or removed.
func TestNoOrphanGoldens(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	known := cases()
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".json")
		if _, ok := known[name]; !ok {
			t.Errorf("orphan golden testdata/%s — delete it or restore its case", e.Name())
		}
	}
}
