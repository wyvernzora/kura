// Package cursor holds the pure-unit list_releases cursor + ref-shape helpers
// (design §6, §13 "Unit"). The cursor is an opaque, server-encoded (base64)
// ordered tuple that BINDS every filter of the request it was issued for — ref,
// path (catalog vs delta), status set, and confidence ceiling — so a
// malformed/undecodable cursor, one issued for a different ref or filter, or a
// cross-path replay (delta cursor without `since`, catalog cursor with `since`)
// is an invalid_cursor error — never silently ignored (design §6). Ref
// shape is validated as namespace:value (malformed => invalid_ref).
//
// These are pure functions the conformance unit tests call directly (design §13).
// At Phase 0.5 every function returns its not-implemented sentinel (or, where a
// bool is the only return, panics) so the cursor-round-trip / ref+path-binding /
// ref-shape goldens fail RED rather than reading a satisfying zero value (design
// plan Phase 0.5). Phase 3 fills in the bodies in place.
package cursor

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"time"
)

// refRe is the §4/§6 ref-shape guard. Refs are opaque — the shape is the
// only thing ever checked; existence against TVDB/kura is never consulted
// (Invariant #4).
var refRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*:.+$`)

// wireCursor is the on-the-wire JSON shape Encode serializes and Decode parses. It
// is a versioned envelope so the opaque string can evolve without a silent
// misparse: a decoded V != cursorVersion is treated as malformed (ErrInvalidCursor).
// Key is carried as RFC3339Nano so the time round-trips to nanosecond precision and
// the seek predicate ((key, infohash) < (cursor.Key, cursor.Infohash)) is exact.
type wireCursor struct {
	V             int      `json:"v"`
	Ref           string   `json:"r"`
	Path          int      `json:"p"`
	Statuses      []string `json:"s,omitempty"`
	MaxConfidence *float64 `json:"c,omitempty"`
	Key           string   `json:"k"`
	Hash          string   `json:"h"`
}

// cursorVersion is the opaque-cursor envelope version. Bumped only on an
// incompatible encoding change; a decoded cursor carrying a different version is
// rejected as malformed.
const cursorVersion = 1

var (
	// ErrInvalidRef is the contract sentinel for a ref that fails the ref shape check.
	ErrInvalidRef = errors.New("indexer/cursor: invalid ref")

	// ErrInvalidCursor is the contract sentinel for a malformed/undecodable cursor,
	// a cursor bound to a different ref, or a cross-path replay.
	ErrInvalidCursor = errors.New("indexer/cursor: invalid cursor")
)

// Path identifies which list_releases ordering the cursor was issued for. The two
// paths sort on different keys, so a cursor encodes its path and the decoder
// rejects a cross-path replay (design §6).
type Path int

const (
	// PathCatalog is the no-`since` catalog scan: orders/cursors on
	// (published_at DESC, infohash DESC) (design §6).
	PathCatalog Path = iota
	// PathDelta is the `since`-present delta scan: orders/cursors on
	// (first_matched_at DESC, infohash DESC) (design §6).
	PathDelta
)

// Binding is the request identity a cursor is issued against. Every filter that
// changes which rows the scan may return lives here, so replaying a page token
// under different filters is rejected rather than silently paging a different
// result set (design §6). Statuses is expected already normalized (sorted and
// deduplicated) by the caller, so two spellings of the same filter compare equal.
type Binding struct {
	Ref           string   // the ref this cursor was issued for (must match the request)
	Path          Path     // catalog vs delta (must match the request's since-presence)
	Statuses      []string // the status filter; empty is the matched-only default
	MaxConfidence *float64 // the confidence ceiling, when the request carried one
}

// Equal reports whether two bindings describe the same filtered scan.
func (b Binding) Equal(other Binding) bool {
	if b.Ref != other.Ref || b.Path != other.Path {
		return false
	}
	if !slices.Equal(b.Statuses, other.Statuses) {
		return false
	}
	switch {
	case b.MaxConfidence == nil && other.MaxConfidence == nil:
		return true
	case b.MaxConfidence == nil || other.MaxConfidence == nil:
		return false
	default:
		return *b.MaxConfidence == *other.MaxConfidence
	}
}

// Cursor is the decoded ordered tuple. The primary key is published_at (catalog)
// or first_matched_at (delta); Infohash is the PK tiebreak that disambiguates the
// non-unique primary sort key so no row is skipped or duplicated across pages
// (design §6). The embedded Binding ties the cursor to its issuing request.
type Cursor struct {
	Binding
	Key      time.Time // published_at (catalog) or first_matched_at (delta)
	Infohash string    // PK tiebreak
}

// Encode serializes a Cursor to the opaque base64 string returned as next_cursor
// (design §6). The Key is encoded as RFC3339Nano so Decode recovers it exactly and
// the (key, infohash) seek predicate is precise. The result is base64(JSON) of the
// versioned wire envelope — opaque to the consumer, never parsed by it.
func Encode(c Cursor) (string, error) {
	payload := wireCursor{
		V:             cursorVersion,
		Ref:           c.Ref,
		Path:          int(c.Path),
		Statuses:      c.Statuses,
		MaxConfidence: c.MaxConfidence,
		Key:           c.Key.Format(time.RFC3339Nano),
		Hash:          c.Infohash,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Decode parses an opaque cursor string and validates that it was issued for the
// expected binding; a malformed/undecodable string, an unexpected version, or any
// binding mismatch is ErrInvalidCursor (design §6). The ref binding rejects a cursor
// replayed against a different ref; the path binding rejects a cross-path replay (a
// delta cursor without `since`, a catalog cursor with `since`), since the two paths
// sort on different keys; the status and confidence bindings reject a cursor replayed
// against a different filter, which would otherwise page a different result set from a
// seek key that never belonged to it. Key is parsed from RFC3339Nano so it round-trips
// exactly.
func Decode(encoded string, want Binding) (Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	var payload wireCursor
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	if payload.V != cursorVersion {
		return Cursor{}, ErrInvalidCursor
	}
	key, err := time.Parse(time.RFC3339Nano, payload.Key)
	if err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	got := Binding{
		Ref:           payload.Ref,
		Path:          Path(payload.Path),
		Statuses:      payload.Statuses,
		MaxConfidence: payload.MaxConfidence,
	}
	// Binding check: a cursor replayed against a different ref, path, status set,
	// or confidence ceiling is invalid — it carries a seek key from a scan the
	// current request is not running (§6).
	if !got.Equal(want) {
		return Cursor{}, ErrInvalidCursor
	}
	return Cursor{Binding: got, Key: key, Infohash: payload.Hash}, nil
}

// ValidateRef checks a ref against the namespace:value shape (design §4/§6). A
// malformed ref returns ErrInvalidRef (wire code "invalid_ref"); a well-formed
// ref returns nil — refs are opaque, never checked for existence (Invariant #4).
func ValidateRef(ref string) error {
	if !refRe.MatchString(ref) {
		return ErrInvalidRef
	}
	return nil
}
