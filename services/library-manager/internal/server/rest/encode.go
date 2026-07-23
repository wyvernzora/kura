package rest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
)

// writeJSON encodes body as JSON with the given status. body=nil
// writes only the status (caller controls Content-Type for
// no-content paths).
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(body)
}

// writeJSONWithETag marshals body, computes a content-based ETag, and
// short-circuits to 304 Not Modified when the client's If-None-Match
// matches. Use for cacheable read endpoints (list, show, trash list).
//
// Marshaling-then-hashing is O(N) twice (encode for hash, encode for
// body). Acceptable: response sizes are bounded and the caller usually
// hits the 304 path on repeat polls anyway. Optimize when measured.
func writeJSONWithETag(w http.ResponseWriter, r *http.Request, status int, body any) {
	buf, err := json.Marshal(body)
	if err != nil {
		writeError(w, err)
		return
	}
	sum := sha256.Sum256(buf)
	tag := `"` + hex.EncodeToString(sum[:8]) + `"`
	w.Header().Set(headerETag, tag)
	w.Header().Set(headerCacheControl, cacheControlReadable)
	if r.Header.Get(headerIfNoneMatch) == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.Copy(w, bytes.NewReader(buf))
	// trailing newline so curl output is readable
	_, _ = w.Write([]byte{'\n'})
}

// decodeJSON decodes from r into dst with strict unknown-field
// rejection. Returns a validationError on malformed JSON or unknown
// fields so handlers don't need to translate decode errors.
func decodeJSON(r io.Reader, dst any) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return &validationError{msg: "invalid request body: " + err.Error()}
	}
	return nil
}
