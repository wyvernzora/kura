package api

// Error is the response body for every non-success status: handler validation,
// unknown routes, unsupported methods, storage failures, and proxy connection
// failures alike.
//
// There is no category field. HTTP status is already the coarse bucket and Kind
// is what clients switch on; a third parallel encoding only creates a way for
// the three to disagree.
//
// This type is deliberately NOT shared with library-manager. Both services emit
// the same three fields and the gateway decodes both; agreement is enforced by
// contract tests rather than by a module edge between two services that
// otherwise share nothing (plan §8.3).
type Error struct {
	Kind    string         `json:"kind"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

// Error kinds emitted by release-indexer. The suite-wide table is closed: a kind
// not listed in the plan's §3.2 may not appear on the wire.
//
// The pre-migration service used a `code` field with `invalid_input` and
// `no_such_release`; those retire into KindInvalidRequest (or a specific
// invalid_* kind) and KindNotFound respectively.
const (
	KindInvalidRequest   = "invalid_request"
	KindMethodNotAllowed = "method_not_allowed"
	KindInvalidRef       = "invalid_ref"
	KindInvalidCursor    = "invalid_cursor"
	KindBatchTooLarge    = "batch_too_large"
	KindNotFound         = "not_found"
	KindNoActiveLease    = "no_active_lease"
	KindStaleLease       = "stale_lease"
	// KindInvalidTransition marks a status change the transition table does
	// not allow (HTTP 409). The message names the attempted transition.
	KindInvalidTransition = "invalid_transition"
	// KindNotMatched marks a magnet fetch for a release that exists but is
	// not matched (HTTP 409). The release is real — 404 would mislead — but
	// the download pipeline is only ever supposed to hold matched releases,
	// so reaching this gate means a stale selection or an upstream bug; the
	// body carries matchStatus so the failure is self-diagnosing.
	KindNotMatched         = "not_matched"
	KindBackendUnavailable = "backend_unavailable"
	KindServerNotReady     = "server_not_ready"
	// KindUpstreamError marks a server-side crawl whose upstream page fetch
	// or parse failed (HTTP 502): the request was fine, the source was not.
	// Retryable by the caller.
	KindUpstreamError = "upstream_error"
	KindInternal      = "internal"
)
