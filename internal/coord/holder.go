// Package coord provides the coordination primitives that protect
// concurrent access to series.json and index.tsv.
//
// Two layers compose:
//
//   - Per-file CAS via content hash. Mutating workflows load a file,
//     compute new state, and atomically write iff the disk file still
//     hashes to the loaded value. The coord package defines the error
//     types and helpers; storage/seriesfile and storage/indexfile own
//     the actual hash + write logic.
//
//   - In-process serialization via Coordinator. Two goroutines in the
//     same process can both pass a hash check and race the rename;
//     the serializing impl (NewMCPCoordinator) holds a per-key mutex
//     around the whole CAS + retry sequence to prevent that. The CLI
//     binary uses NewCLICoordinator (no-op) since each invocation runs
//     a single goroutine.
//
// One workflow (reconcile apply) holds an explicit claim recorded in
// series.json's in_progress field; everything else relies on hash CAS
// alone. See plan/locking.md for the full design.
package coord

import "time"

// Holder identifies the process holding a series claim (in_progress on
// series.json). Currently set only by reconcile apply; the schema is
// kept generic so future claim-holding workflows can add Op values.
type Holder struct {
	Op      string    `json:"op"`
	Token   string    `json:"token,omitempty"`
	PID     int       `json:"pid"`
	Host    string    `json:"host"`
	Started time.Time `json:"started"`
}

// Mutator stamps the most recent successful CAS write for diagnostics
// (last_mutated on series.json and the header line on index.tsv).
// Surfaces in ConflictError messages so a losing writer can identify
// who won the race.
type Mutator struct {
	Op   string    `json:"op"`
	PID  int       `json:"pid"`
	Host string    `json:"host"`
	At   time.Time `json:"at"`
}

// HolderData renders a Holder into the map shape used by structured
// surface error payloads (errkind.Data).
func HolderData(h Holder) map[string]any {
	out := map[string]any{
		"op":      h.Op,
		"pid":     h.PID,
		"host":    h.Host,
		"started": h.Started.UTC().Format(time.RFC3339),
	}
	if h.Token != "" {
		out["token"] = h.Token
	}
	return out
}

// MutatorData renders a Mutator into the map shape used by structured
// surface error payloads (errkind.Data).
func MutatorData(m Mutator) map[string]any {
	if m.Op == "" {
		return map[string]any{}
	}
	return map[string]any{
		"op":   m.Op,
		"pid":  m.PID,
		"host": m.Host,
		"at":   m.At.UTC().Format(time.RFC3339),
	}
}
