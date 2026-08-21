package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/internal/store"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

type fakeStore struct {
	claim           store.ClaimParams
	submit          store.SubmitParams
	ingest          []store.IngestParams
	ingestOutcome   store.IngestOutcome
	ingestErr       error
	queueStats      store.QueueStats
	queueStatsErr   error
	infohashes      []string
	releaseInfohash string
	releaseQuery    store.ReleaseQuery
	releasePage     store.ReleasePage
	setStatus       store.SetStatusParams
	setStatusErr    error
	noMagnets       bool
	releaseDetail   store.ReleaseDetail
	releaseErr      error
}

func (f *fakeStore) Ping(context.Context) error { return nil }
func (f *fakeStore) IngestN(_ context.Context, p store.IngestParams) (store.IngestOutcome, error) {
	f.ingest = append(f.ingest, p)
	return f.ingestOutcome, f.ingestErr
}
func (f *fakeStore) Claim(_ context.Context, p store.ClaimParams) (store.ClaimResult, error) {
	f.claim = p
	return store.ClaimResult{
		Items: []store.ClaimedRelease{{
			Infohash:     "0123456789abcdef0123456789abcdef01234567",
			ClaimToken:   1,
			AttemptCount: 1,
			LeaseExpires: time.Unix(1, 0).UTC(),
		}},
	}, nil
}
func (f *fakeStore) Submit(_ context.Context, p store.SubmitParams) error {
	f.submit = p
	return nil
}
func (f *fakeStore) SetStatus(_ context.Context, p store.SetStatusParams) error {
	f.setStatus = p
	return f.setStatusErr
}
func (f *fakeStore) QueueStats(context.Context) (store.QueueStats, error) {
	return f.queueStats, f.queueStatsErr
}
func (f *fakeStore) CatalogStats(context.Context) (store.CatalogStats, error) {
	return store.CatalogStats{}, nil
}
func (f *fakeStore) ListReleases(_ context.Context, q store.ReleaseQuery) (store.ReleasePage, error) {
	f.releaseQuery = q
	return f.releasePage, nil
}
func (f *fakeStore) GetRelease(_ context.Context, infohash string) (store.ReleaseDetail, error) {
	f.releaseInfohash = infohash
	if f.releaseErr != nil {
		return store.ReleaseDetail{}, f.releaseErr
	}
	if f.releaseDetail.Infohash != "" {
		return f.releaseDetail, nil
	}
	return store.ReleaseDetail{
		Infohash:    infohash,
		Title:       "release",
		PublishedAt: time.Unix(1, 0).UTC(),
		MatchStatus: "unmatched",
		CreatedAt:   time.Unix(1, 0).UTC(),
		UpdatedAt:   time.Unix(1, 0).UTC(),
	}, nil
}
func (f *fakeStore) ResolveMagnets(_ context.Context, infohashes []string) (map[string]string, error) {
	f.infohashes = infohashes
	if f.noMagnets {
		return map[string]string{}, nil
	}
	return map[string]string{
		"0123456789abcdef0123456789abcdef01234567": "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&tr=udp://tracker",
	}, nil
}
func (f *fakeStore) Close() error { return nil }

func TestClaimRejectsInvalidJSON(t *testing.T) {
	st := &fakeStore{}
	for _, body := range []string{"", "{"} {
		req := httptest.NewRequest(http.MethodPost, "/api/releases/v1/queue/claim", strings.NewReader(body))
		rec := httptest.NewRecorder()

		New(st).ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q: status = %d, want %d; response %s", body, rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	}
}

func TestSubmitRejectsInvalidStatus(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/releases/v1/queue/submit", strings.NewReader(`{
		"infohash":"0123456789abcdef0123456789abcdef01234567",
		"claimToken":1,
		"status":"defer"
	}`))
	rec := httptest.NewRecorder()

	New(&fakeStore{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; response %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestSubmitPreservesSuppressedConfidence(t *testing.T) {
	st := &fakeStore{}
	req := httptest.NewRequest(http.MethodPost, "/api/releases/v1/queue/submit", strings.NewReader(`{
		"infohash":"0123456789abcdef0123456789abcdef01234567",
		"claimToken":1,
		"status":"suppressed",
		"confidence":0.73
	}`))
	rec := httptest.NewRecorder()

	New(st).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; response %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if st.submit.Confidence == nil || *st.submit.Confidence != 0.73 {
		t.Fatalf("confidence = %v, want 0.73", st.submit.Confidence)
	}
}

func TestGetMagnet(t *testing.T) {
	st := &fakeStore{}
	req := httptest.NewRequest(http.MethodGet, "/api/releases/v1/0123456789abcdef0123456789abcdef01234567/magnet", http.NoBody)
	rec := httptest.NewRecorder()

	New(st).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; response %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(st.infohashes) != 1 || st.infohashes[0] != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("infohashes = %#v", st.infohashes)
	}
	var body struct {
		Infohash string `json:"infohash"`
		Magnet   string `json:"magnet"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Infohash != "0123456789abcdef0123456789abcdef01234567" || body.Magnet != "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&tr=udp://tracker" {
		t.Fatalf("response = %+v", body)
	}
}

func TestGetRelease(t *testing.T) {
	st := &fakeStore{}
	req := httptest.NewRequest(http.MethodGet, "/api/releases/v1/JN7DUBILGVIFL46NCSOF42GZ5JCXNNUS", http.NoBody)
	rec := httptest.NewRecorder()

	New(st).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; response %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if st.releaseInfohash != "4b7e3a050b355055f3cd149c5e68d9ea4576b692" {
		t.Fatalf("release infohash = %q, want canonical hex", st.releaseInfohash)
	}
	var body struct {
		Infohash    string `json:"infohash"`
		Title       string `json:"title"`
		MatchStatus string `json:"matchStatus"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Infohash != "4b7e3a050b355055f3cd149c5e68d9ea4576b692" || body.Title != "release" || body.MatchStatus != "unmatched" {
		t.Fatalf("response = %+v", body)
	}
}

func TestGetReleaseRejectsInvalidInfohash(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/releases/v1/not-an-infohash", http.NoBody)
	rec := httptest.NewRecorder()

	New(&fakeStore{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; response %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	var body struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Kind != "invalid_request" || body.Message != "invalid infohash" {
		t.Fatalf("response = %+v", body)
	}
}

func TestSetStatusRoutesToStore(t *testing.T) {
	st := &fakeStore{}
	req := httptest.NewRequest(http.MethodPut,
		"/api/releases/v1/0123456789ABCDEF0123456789abcdef01234567/status",
		strings.NewReader(`{"status":"dead","reason":"stalled at 0%"}`))
	rec := httptest.NewRecorder()

	New(st).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; response %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	// The path infohash is normalized before it reaches the store, so an
	// uppercase caller cannot write a row key the rest of the service
	// would never match.
	if st.setStatus.Infohash != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("infohash = %q, want the lowercased form", st.setStatus.Infohash)
	}
	if st.setStatus.Status != "dead" || st.setStatus.Reason != "stalled at 0%" {
		t.Fatalf("params = %+v, want dead with the reason preserved", st.setStatus)
	}
}

func TestSetStatusRejectsStatusOutsideTheVocabulary(t *testing.T) {
	st := &fakeStore{}
	req := httptest.NewRequest(http.MethodPut,
		"/api/releases/v1/0123456789abcdef0123456789abcdef01234567/status",
		strings.NewReader(`{"status":"defer"}`))
	rec := httptest.NewRecorder()

	New(st).ServeHTTP(rec, req)

	// A label outside the closed MatchStatus vocabulary is refused at the
	// boundary; which transitions each real status offers is the store
	// table's decision.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; response %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if st.setStatus.Status != "" {
		t.Fatalf("store was called with %+v, want no call", st.setStatus)
	}
}

func TestSetStatusReportsInvalidTransitionAsConflict(t *testing.T) {
	st := &fakeStore{setStatusErr: fmt.Errorf("unmatched -> dead: %w", store.ErrInvalidTransition)}
	req := httptest.NewRequest(http.MethodPut,
		"/api/releases/v1/0123456789abcdef0123456789abcdef01234567/status",
		strings.NewReader(`{"status":"dead"}`))
	rec := httptest.NewRecorder()

	New(st).ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; response %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	var body api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Kind != api.KindInvalidTransition {
		t.Fatalf("kind = %q, want %q", body.Kind, api.KindInvalidTransition)
	}
	// The message names both ends so an operator can see what was refused.
	if !strings.Contains(body.Message, "unmatched -> dead") {
		t.Fatalf("message = %q, want it to name the attempted transition", body.Message)
	}
}

// The hand-match body carries the ref the operator picked; the endpoint passes
// it down untouched alongside the normalized infohash.
func TestSetStatusForwardsTheHandMatchRef(t *testing.T) {
	st := &fakeStore{}
	req := httptest.NewRequest(http.MethodPut,
		"/api/releases/v1/0123456789abcdef0123456789abcdef01234567/status",
		strings.NewReader(`{"status":"matched","ref":"tvdb:370070","reason":"hand matched"}`))
	rec := httptest.NewRecorder()

	New(st).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; response %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if st.setStatus.Status != "matched" || st.setStatus.Ref != "tvdb:370070" {
		t.Fatalf("params = %+v, want matched with the ref preserved", st.setStatus)
	}
}

// Requeue is the other operator row: exhausted back to unmatched, no ref.
func TestSetStatusForwardsRequeue(t *testing.T) {
	st := &fakeStore{}
	req := httptest.NewRequest(http.MethodPut,
		"/api/releases/v1/0123456789abcdef0123456789abcdef01234567/status",
		strings.NewReader(`{"status":"unmatched","reason":"new source posting"}`))
	rec := httptest.NewRecorder()

	New(st).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; response %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if st.setStatus.Status != "unmatched" || st.setStatus.Ref != "" {
		t.Fatalf("params = %+v, want unmatched with no ref", st.setStatus)
	}
}

// A ref rule broken is a bad request, not a conflict: the transition was on
// offer, the body was wrong.
func TestSetStatusReportsRefRuleViolationsAsBadRequest(t *testing.T) {
	for _, sentinel := range []error{store.ErrRefRequired, store.ErrRefForbidden} {
		t.Run(sentinel.Error(), func(t *testing.T) {
			st := &fakeStore{setStatusErr: fmt.Errorf("set_status abc: %w", sentinel)}
			req := httptest.NewRequest(http.MethodPut,
				"/api/releases/v1/0123456789abcdef0123456789abcdef01234567/status",
				strings.NewReader(`{"status":"matched","ref":"tvdb:1"}`))
			rec := httptest.NewRecorder()

			New(st).ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; response %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			var body api.Error
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Kind != api.KindInvalidRequest {
				t.Fatalf("kind = %q, want %q", body.Kind, api.KindInvalidRequest)
			}
		})
	}
}

func TestSetStatusRejectsNonPut(t *testing.T) {
	rec := httptest.NewRecorder()
	New(&fakeStore{}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
		"/api/releases/v1/0123456789abcdef0123456789abcdef01234567/status",
		strings.NewReader(`{"status":"dead"}`)))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestGetMagnetGatedReleaseIsConflictNotMissing(t *testing.T) {
	// The fake resolves no magnets but knows the release, which is exactly
	// the gate case: the row exists, its status forbids serving the magnet.
	st := &fakeStore{noMagnets: true, releaseDetail: store.ReleaseDetail{
		Infohash:    "0123456789abcdef0123456789abcdef01234567",
		MatchStatus: "dead",
	}}
	rec := httptest.NewRecorder()
	New(st).ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/releases/v1/0123456789abcdef0123456789abcdef01234567/magnet", http.NoBody))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; response %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	var body api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Kind != api.KindNotMatched {
		t.Fatalf("kind = %q, want %q", body.Kind, api.KindNotMatched)
	}
	// matchStatus in the body is what makes the failure self-diagnosing in
	// an error-workflow payload: "the pruner got here first" vs a bug hunt.
	if body.Data["matchStatus"] != "dead" {
		t.Fatalf("data = %v, want matchStatus dead", body.Data)
	}
}

func TestGetMagnetUnknownInfohashStaysNotFound(t *testing.T) {
	st := &fakeStore{noMagnets: true, releaseErr: store.ErrNoSuchRelease}
	rec := httptest.NewRecorder()
	New(st).ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/releases/v1/0123456789abcdef0123456789abcdef01234567/magnet", http.NoBody))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; response %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestGetMagnetStoreFailureIsInternalNotMissing(t *testing.T) {
	// A resolve miss plus a failing detail lookup is a database problem, not
	// an unknown infohash: reporting 404 would make the pipeline drop a
	// release that exists.
	st := &fakeStore{noMagnets: true, releaseErr: fmt.Errorf("pool acquire: %w", context.DeadlineExceeded)}
	rec := httptest.NewRecorder()
	New(st).ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/releases/v1/0123456789abcdef0123456789abcdef01234567/magnet", http.NoBody))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; response %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	var body api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Kind != api.KindInternal {
		t.Fatalf("kind = %q, want %q", body.Kind, api.KindInternal)
	}
}
