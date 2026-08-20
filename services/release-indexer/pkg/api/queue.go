package api

import "time"

// SubmitStatus is the closed set of dispositions a matcher may submit.
type SubmitStatus string

const (
	SubmitStatusMatched    SubmitStatus = "matched"
	SubmitStatusUnmatched  SubmitStatus = "unmatched"
	SubmitStatusSuppressed SubmitStatus = "suppressed"
)

// ClaimRequest is the POST /api/releases/v1/queue/claim body.
//
// Limit defaults to 1 and is capped at 500. LeaseSeconds is required: a claim
// without a lease would hold work indefinitely on a crashed worker.
type ClaimRequest struct {
	Limit        int `json:"limit,omitempty"`
	LeaseSeconds int `json:"leaseSeconds"`
}

// ClaimItem is one leased release. ClaimToken fences the later submit: a
// disposition carrying a stale token is rejected rather than applied.
type ClaimItem struct {
	Infohash       string         `json:"infohash"`
	ClaimToken     int64          `json:"claimToken"`
	AttemptCount   int            `json:"attemptCount"`
	LeaseExpiresAt time.Time      `json:"leaseExpiresAt"`
	RawItems       []ClaimRawItem `json:"rawItems"`
}

// ClaimRawItem is the evidence shipped with a claim. It is deliberately
// narrower than RawItem: a matcher needs the posting text, not its ingest
// bookkeeping.
type ClaimRawItem struct {
	ID          int64     `json:"id"`
	Source      string    `json:"source"`
	SourceID    string    `json:"sourceId"`
	Title       string    `json:"title"`
	URL         *string   `json:"url"`
	PublishedAt time.Time `json:"publishedAt"`
}

// ClaimResponse is the claim result. Items is never null; an empty queue
// returns an empty array.
type ClaimResponse struct {
	Items []ClaimItem `json:"items"`
}

// SubmitRequest is the POST /api/releases/v1/queue/submit body.
//
// Ref is required when Status is matched and meaningless otherwise. Confidence
// is optional even for a match — an external matcher that does not score its
// own output is valid input, and a missing score is not a zero score.
type SubmitRequest struct {
	Infohash   string       `json:"infohash"`
	ClaimToken int64        `json:"claimToken"`
	Status     SubmitStatus `json:"status"`
	Ref        string       `json:"ref,omitempty"`
	Confidence *float64     `json:"confidence,omitempty"`
	Reason     string       `json:"reason,omitempty"`
}

// SubmitResponse is a bare acknowledgement.
//
// A richer body (resulting matchStatus and attemptCount, per §3.1's "mutations
// return resulting state") would need store.Submit to return the updated row —
// a signature and SQL change no phase scopes. Left as-is deliberately.
type SubmitResponse struct {
	Ok bool `json:"ok"`
}

// QueueStats is the GET /api/releases/v1/queue/stats response.
//
// Exhausted is the operator intervention signal: those releases will not be
// re-offered without a deliberate reset.
type QueueStats struct {
	Available  int `json:"available"`
	Leased     int `json:"leased"`
	Unmatched  int `json:"unmatched"`
	Matched    int `json:"matched"`
	Suppressed int `json:"suppressed"`
	Exhausted  int `json:"exhausted"`
	Dead       int `json:"dead"`
}
