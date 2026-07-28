package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/internal/cursor"
	"github.com/wyvernzora/kura/services/release-indexer/internal/infohash"
	"github.com/wyvernzora/kura/services/release-indexer/internal/store"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

var ErrInvalidInput = errors.New("takuhai/dispatch: invalid input")

type Dispatcher struct {
	store store.Store
}

func New(s store.Store) *Dispatcher { return &Dispatcher{store: s} }

type ListReleasesRequest struct {
	Ref    string     `json:"ref,omitempty" jsonschema:"optional opaque metadata ref in namespace:value form; omit to list recent matched releases across all refs"`
	Since  *time.Time `json:"since,omitempty" jsonschema:"RFC3339 timestamp; when present, page by first matched time"`
	Limit  int        `json:"limit,omitempty" jsonschema:"maximum releases to return; server defaults and caps apply"`
	Cursor string     `json:"cursor,omitempty" jsonschema:"opaque next_cursor from the previous response"`
}

type GetReleaseRequest struct {
	Infohash string `json:"infohash" jsonschema:"canonical v1 btih infohash for the release to fetch"`
}

type ResolveMagnetsRequest struct {
	Infohashes []string `json:"infohashes"`
}

type ResolveMagnetsResult struct {
	Magnets map[string]string `json:"magnets"`
}

func (d *Dispatcher) Claim(ctx context.Context, input []byte) ([]byte, error) {
	var req api.ClaimRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	out, err := d.ClaimTyped(ctx, req)
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

func (d *Dispatcher) ClaimTyped(ctx context.Context, req api.ClaimRequest) (api.ClaimResponse, error) {
	res, err := d.store.Claim(ctx, store.ClaimParams{
		Limit:        req.Limit,
		LeaseSeconds: req.LeaseSeconds,
	})
	if err != nil {
		return api.ClaimResponse{}, err
	}
	out := api.ClaimResponse{Items: make([]api.ClaimItem, 0, len(res.Items))}
	for _, it := range res.Items {
		raw := make([]api.ClaimRawItem, 0, len(it.RawItems))
		for _, ri := range it.RawItems {
			raw = append(raw, api.ClaimRawItem{
				ID:          ri.ID,
				Source:      ri.Source,
				SourceID:    ri.SourceID,
				Title:       ri.Title,
				URL:         ri.URL,
				PublishedAt: ri.PublishedAt,
			})
		}
		out.Items = append(out.Items, api.ClaimItem{
			Infohash:       it.Infohash,
			ClaimToken:     it.ClaimToken,
			AttemptCount:   it.AttemptCount,
			LeaseExpiresAt: it.LeaseExpires,
			RawItems:       raw,
		})
	}
	return out, nil
}

func (d *Dispatcher) Submit(ctx context.Context, input []byte) ([]byte, error) {
	var req api.SubmitRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	if err := d.SubmitTyped(ctx, req); err != nil {
		return nil, err
	}
	return []byte(`{"ok":true}`), nil
}

func (d *Dispatcher) SubmitTyped(ctx context.Context, req api.SubmitRequest) error {
	switch req.Status {
	case api.SubmitStatusMatched, api.SubmitStatusUnmatched, api.SubmitStatusSuppressed:
	default:
		return fmt.Errorf("%w: invalid status %q", ErrInvalidInput, req.Status)
	}
	if err := d.store.Submit(ctx, store.SubmitParams{
		Infohash:   req.Infohash,
		ClaimToken: req.ClaimToken,
		Status:     string(req.Status),
		Ref:        req.Ref,
		Confidence: req.Confidence,
		Reason:     req.Reason,
	}); err != nil {
		return err
	}
	return nil
}

func (d *Dispatcher) QueueStats(ctx context.Context, input []byte) ([]byte, error) {
	out, err := d.QueueStatsTyped(ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

func (d *Dispatcher) QueueStatsTyped(ctx context.Context) (api.QueueStats, error) {
	qs, err := d.store.QueueStats(ctx)
	if err != nil {
		return api.QueueStats{}, err
	}
	return api.QueueStats{
		Available:  qs.Available,
		Leased:     qs.Leased,
		Unmatched:  qs.Unmatched,
		Matched:    qs.Matched,
		Suppressed: qs.Suppressed,
		Exhausted:  qs.Exhausted,
	}, nil
}

func (d *Dispatcher) ListReleases(ctx context.Context, input []byte) ([]byte, error) {
	var req ListReleasesRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	out, err := d.ListReleasesTyped(ctx, req)
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

func (d *Dispatcher) ListReleasesTyped(ctx context.Context, req ListReleasesRequest) (api.ReleaseList, error) {
	page, err := d.store.ListReleases(ctx, store.ReleaseQuery{
		Ref:    req.Ref,
		Since:  req.Since,
		Limit:  req.Limit,
		Cursor: req.Cursor,
	})
	if err != nil {
		return api.ReleaseList{}, err
	}
	out := api.ReleaseList{Items: make([]api.ReleaseItem, 0, len(page.Releases))}
	for _, r := range page.Releases {
		out.Items = append(out.Items, api.ReleaseItem{
			Infohash:    r.Infohash,
			Ref:         r.Ref,
			Title:       r.Title,
			SizeBytes:   r.SizeBytes,
			PublishedAt: r.PublishedAt,
			Confidence:  r.Confidence,
			Sources:     r.Sources,
		})
	}
	if page.NextCursor != "" {
		nc := page.NextCursor
		out.NextCursor = &nc
	}
	return out, nil
}

func (d *Dispatcher) GetRelease(ctx context.Context, input []byte) ([]byte, error) {
	var req GetReleaseRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	out, err := d.GetReleaseTyped(ctx, req)
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

func (d *Dispatcher) GetReleaseTyped(ctx context.Context, req GetReleaseRequest) (api.ReleaseDetail, error) {
	ih, err := infohash.NormalizeInfohash(req.Infohash)
	if err != nil {
		return api.ReleaseDetail{}, fmt.Errorf("%w: invalid infohash %q: %v", ErrInvalidInput, req.Infohash, err)
	}
	detail, err := d.store.GetRelease(ctx, ih)
	if err != nil {
		return api.ReleaseDetail{}, err
	}
	sources := detail.Sources
	if sources == nil {
		sources = []string{}
	}
	out := api.ReleaseDetail{
		Infohash:       detail.Infohash,
		Title:          detail.Title,
		Magnet:         detail.Magnet,
		SizeBytes:      detail.SizeBytes,
		PublishedAt:    detail.PublishedAt,
		Sources:        sources,
		MatchStatus:    api.MatchStatus(detail.MatchStatus),
		Ref:            detail.Ref,
		Confidence:     detail.Confidence,
		FirstMatchedAt: detail.FirstMatchedAt,
		AttemptCount:   detail.AttemptCount,
		CreatedAt:      detail.CreatedAt,
		UpdatedAt:      detail.UpdatedAt,
		RawItems:       make([]api.RawItem, 0, len(detail.RawItems)),
		MatchEvents:    make([]api.MatchEvent, 0, len(detail.MatchEvents)),
	}
	for _, item := range detail.RawItems {
		out.RawItems = append(out.RawItems, api.RawItem{
			ID:          item.ID,
			Source:      item.Source,
			SourceID:    item.SourceID,
			Title:       item.Title,
			URL:         item.URL,
			PublishedAt: item.PublishedAt,
			IngestedAt:  item.IngestedAt,
		})
	}
	for _, ev := range detail.MatchEvents {
		out.MatchEvents = append(out.MatchEvents, api.MatchEvent{
			ID:         ev.ID,
			Status:     api.MatchStatus(ev.Status),
			Ref:        ev.Ref,
			Confidence: ev.Confidence,
			Reason:     ev.Reason,
			CreatedAt:  ev.CreatedAt,
		})
	}
	return out, nil
}

func (d *Dispatcher) ResolveMagnets(ctx context.Context, input []byte) ([]byte, error) {
	var req ResolveMagnetsRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	out, err := d.ResolveMagnetsTyped(ctx, req)
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

func (d *Dispatcher) ResolveMagnetsTyped(ctx context.Context, req ResolveMagnetsRequest) (ResolveMagnetsResult, error) {
	magnets, err := d.store.ResolveMagnets(ctx, req.Infohashes)
	if err != nil {
		return ResolveMagnetsResult{}, err
	}
	if magnets == nil {
		magnets = map[string]string{}
	}
	return ResolveMagnetsResult{Magnets: magnets}, nil
}

// ErrorKind maps a sentinel to its wire kind. An empty result means the error
// is not one this service models — the caller reports it as internal and does
// not echo its message.
//
// The pre-migration codes `no_such_release` and `invalid_input` are retired in
// favour of the suite-wide `not_found` and `invalid_request` (plan §3.2).
func ErrorKind(err error) string {
	switch {
	case errors.Is(err, store.ErrNoSuchRelease):
		return api.KindNotFound
	case errors.Is(err, store.ErrNoActiveLease):
		return api.KindNoActiveLease
	case errors.Is(err, store.ErrStaleLease):
		return api.KindStaleLease
	case errors.Is(err, cursor.ErrInvalidRef):
		return api.KindInvalidRef
	case errors.Is(err, cursor.ErrInvalidCursor):
		return api.KindInvalidCursor
	case errors.Is(err, ErrInvalidInput):
		return api.KindInvalidRequest
	default:
		return ""
	}
}

func timeStringPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339Nano)
	return &s
}
