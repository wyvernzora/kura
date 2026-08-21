package cursor

import (
	"errors"
	"testing"
	"time"
)

func testKey() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 123456789, time.UTC) }

// A cursor carries every filter it was issued under, so the page it resumes is
// the page the caller is still asking for.
func TestEncodeDecodeRoundTripsTheWholeBinding(t *testing.T) {
	want := Binding{
		Ref:           "tvdb:123",
		Path:          PathCatalog,
		Statuses:      []string{"exhausted", "suppressed"},
		MaxConfidence: ptr(0.75),
	}
	enc, err := Encode(Cursor{Binding: want, Key: testKey(), Infohash: "abc"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(enc, want)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !got.Binding.Equal(want) {
		t.Fatalf("binding = %+v, want %+v", got.Binding, want)
	}
	if !got.Key.Equal(testKey()) || got.Infohash != "abc" {
		t.Fatalf("seek = (%v, %q), want (%v, abc)", got.Key, got.Infohash, testKey())
	}
}

// Replaying a page token under different filters would seek with a key that
// never belonged to the new scan, silently skipping or repeating rows.
func TestDecodeRejectsABindingTheCursorWasNotIssuedFor(t *testing.T) {
	issued := Binding{
		Ref:           "tvdb:123",
		Path:          PathCatalog,
		Statuses:      []string{"exhausted", "suppressed"},
		MaxConfidence: ptr(0.75),
	}
	enc, err := Encode(Cursor{Binding: issued, Key: testKey(), Infohash: "abc"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	for name, replay := range map[string]Binding{
		"different ref":            {Ref: "tvdb:999", Path: PathCatalog, Statuses: []string{"exhausted", "suppressed"}, MaxConfidence: ptr(0.75)},
		"different path":           {Ref: "tvdb:123", Path: PathDelta, Statuses: []string{"exhausted", "suppressed"}, MaxConfidence: ptr(0.75)},
		"narrowed status set":      {Ref: "tvdb:123", Path: PathCatalog, Statuses: []string{"exhausted"}, MaxConfidence: ptr(0.75)},
		"widened status set":       {Ref: "tvdb:123", Path: PathCatalog, Statuses: []string{"dead", "exhausted", "suppressed"}, MaxConfidence: ptr(0.75)},
		"dropped status filter":    {Ref: "tvdb:123", Path: PathCatalog, MaxConfidence: ptr(0.75)},
		"different maxConfidence":  {Ref: "tvdb:123", Path: PathCatalog, Statuses: []string{"exhausted", "suppressed"}, MaxConfidence: ptr(0.9)},
		"dropped maxConfidence":    {Ref: "tvdb:123", Path: PathCatalog, Statuses: []string{"exhausted", "suppressed"}},
		"everything else the same": {Ref: "tvdb:123", Path: PathCatalog, Statuses: []string{"exhausted", "suppressed"}, MaxConfidence: ptr(0.750001)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(enc, replay); !errors.Is(err, ErrInvalidCursor) {
				t.Fatalf("Decode err = %v, want ErrInvalidCursor", err)
			}
		})
	}
}

// The unfiltered default must keep round-tripping: an absent status filter and
// an empty one are the same request, and neither may become a mismatch.
func TestDecodeTreatsNilAndEmptyStatusesAsTheSameBinding(t *testing.T) {
	enc, err := Encode(Cursor{
		Binding:  Binding{Ref: "", Path: PathCatalog, Statuses: []string{}},
		Key:      testKey(),
		Infohash: "abc",
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if _, err := Decode(enc, Binding{Ref: "", Path: PathCatalog}); err != nil {
		t.Fatalf("Decode of the default binding: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }
