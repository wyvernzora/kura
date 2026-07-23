package media

import "time"

// Record is one tracked media file (active or staged) with the facts Kura
// persists about it.
//
// JSON tags are transitional and get dropped once response types in
// internal/response/ absorb the serialization concern.
type Record struct {
	Path       string      `json:"path"`
	Source     Source      `json:"source"`
	Resolution Resolution  `json:"resolution"`
	Codec      Codec       `json:"codec,omitempty"`
	Size       int64       `json:"size"`
	MTime      time.Time   `json:"mtime"`
	Companions []Companion `json:"companions"`
	Attrs      Attrs       `json:"attrs,omitempty"`
}

// Companion is one tracked file alongside the primary media file (external
// subtitle, audio track, etc.).
type Companion struct {
	Path     string    `json:"path"`
	Role     string    `json:"role,omitempty"`
	Language string    `json:"language,omitempty"`
	Label    string    `json:"label,omitempty"`
	Size     int64     `json:"size"`
	MTime    time.Time `json:"mtime"`
}

// CloneRecord returns a deep copy of in so callers can mutate it without
// disturbing shared state. Companions slice is always non-nil after clone.
func CloneRecord(in Record) Record {
	out := in
	out.Companions = append([]Companion(nil), in.Companions...)
	if out.Companions == nil {
		out.Companions = []Companion{}
	}
	out.Attrs = CloneAttrs(in.Attrs)
	return out
}
