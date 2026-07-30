package seriesmeta_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wyvernzora/kura/services/tape-backup/internal/seriesmeta"
)

func TestReadReturnsPartialMetadata(t *testing.T) {
	path := writeSeriesJSON(t, `{
		"schemaVersion": 3,
		"generation": 7,
		"metadataRef": "tvdb:370070",
		"preferredTitle": "Sousou no Frieren",
		"episodes": {
			"S01E01": {"active": {"path": "ignored"}}
		},
		"stagedTrash": [],
		"stagedExtras": []
	}`)

	got, err := seriesmeta.Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Generation != 7 {
		t.Fatalf("Generation = %d, want 7", got.Generation)
	}
	if got.MetadataRef != "tvdb:370070" {
		t.Fatalf("MetadataRef = %q, want %q", got.MetadataRef, "tvdb:370070")
	}
	if got.HasStagedIntent {
		t.Fatal("HasStagedIntent = true, want false")
	}
	if got.HasActiveClaim {
		t.Fatal("HasActiveClaim = true, want false")
	}
}

// These mixed-case fixtures guard the index-snapshot decode path. The on-disk
// path rejects these spellings first because series_v3.json forbids extra keys.
func TestReadAgreesWithSeriesfileFieldMatching(t *testing.T) {
	cases := []struct {
		name string
		data string
		want seriesmeta.Metadata
	}{
		{
			name: "canonical spellings",
			data: `{
				"schemaVersion": 3,
				"generation": 9,
				"metadataRef": "tvdb:1",
				"episodes": {"S01E01": {"staged": {}}},
				"stagedTrash": [],
				"stagedExtras": [],
				"in_progress": {}
			}`,
			want: seriesmeta.Metadata{
				Generation:      9,
				MetadataRef:     "tvdb:1",
				HasStagedIntent: true,
				HasActiveClaim:  true,
			},
		},
		{
			name: "title case generation",
			data: `{"schemaVersion":3,"Generation":5}`,
			want: seriesmeta.Metadata{Generation: 5},
		},
		{
			name: "title case schema version",
			data: `{"SchemaVersion":3}`,
			want: seriesmeta.Metadata{Generation: 1},
		},
		{
			name: "upper case generation",
			data: `{"schemaVersion":3,"GENERATION":7}`,
			want: seriesmeta.Metadata{Generation: 7},
		},
		{
			name: "title case staged trash",
			data: `{"schemaVersion":3,"StagedTrash":[{}]}`,
			want: seriesmeta.Metadata{
				Generation:      1,
				HasStagedIntent: true,
			},
		},
		{
			name: "lower case staged trash",
			data: `{"schemaVersion":3,"stagedtrash":[{}]}`,
			want: seriesmeta.Metadata{
				Generation:      1,
				HasStagedIntent: true,
			},
		},
		{
			name: "title case active claim",
			data: `{"schemaVersion":3,"In_Progress":{}}`,
			want: seriesmeta.Metadata{
				Generation:     1,
				HasActiveClaim: true,
			},
		},
		{
			name: "title case metadata ref",
			data: `{"schemaVersion":3,"MetadataRef":"tvdb:1"}`,
			want: seriesmeta.Metadata{
				Generation:  1,
				MetadataRef: "tvdb:1",
			},
		},
		{
			name: "duplicate generation is last wins",
			data: `{"schemaVersion":3,"generation":1,"Generation":5}`,
			want: seriesmeta.Metadata{Generation: 5},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := seriesmeta.Read(writeSeriesJSON(t, tc.data))
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Read = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestReadNormalizesGeneration(t *testing.T) {
	cases := []struct {
		name       string
		generation string
		want       int
	}{
		{name: "absent", want: 1},
		{name: "null", generation: `"generation": null,`, want: 1},
		{name: "zero", generation: `"generation": 0,`, want: 1},
		{name: "negative", generation: `"generation": -4,`, want: 1},
		{name: "one", generation: `"generation": 1,`, want: 1},
		{name: "positive", generation: `"generation": 5,`, want: 5},
		{name: "title case", generation: `"Generation": 5,`, want: 5},
		{name: "upper case", generation: `"GENERATION": 7,`, want: 7},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSeriesJSON(
				t,
				`{"schemaVersion":3,`+tc.generation+`"metadataRef":"tvdb:370070"}`,
			)
			got, err := seriesmeta.Read(path)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if got.Generation != tc.want {
				t.Fatalf("Generation = %d, want %d", got.Generation, tc.want)
			}
		})
	}
}

func TestReadDetectsEpisodeStage(t *testing.T) {
	cases := []struct {
		name     string
		episodes string
		want     bool
	}{
		{name: "staged object", episodes: `"S01E01":{"staged":{}}`, want: true},
		{name: "staged null", episodes: `"S01E01":{"staged":null}`},
		{name: "staged absent", episodes: `"S01E01":{}`},
		{name: "title case staged", episodes: `"S01E01":{"Staged":{}}`, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSeriesJSON(
				t,
				`{"schemaVersion":3,"metadataRef":"tvdb:370070","episodes":{`+
					tc.episodes+`}}`,
			)
			got, err := seriesmeta.Read(path)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if got.HasStagedIntent != tc.want {
				t.Fatalf("HasStagedIntent = %t, want %t", got.HasStagedIntent, tc.want)
			}
		})
	}
}

func TestReadDetectsStagedTrash(t *testing.T) {
	cases := []struct {
		name  string
		field string
		want  bool
	}{
		{name: "non-empty", field: `"stagedTrash":[{}],`, want: true},
		{name: "empty", field: `"stagedTrash":[],`},
		{name: "null", field: `"stagedTrash":null,`},
		{name: "absent"},
		{name: "title case", field: `"StagedTrash":[{}],`, want: true},
		{name: "lower case", field: `"stagedtrash":[{}],`, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSeriesJSON(
				t,
				`{"schemaVersion":3,`+tc.field+`"metadataRef":"tvdb:370070"}`,
			)
			got, err := seriesmeta.Read(path)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if got.HasStagedIntent != tc.want {
				t.Fatalf("HasStagedIntent = %t, want %t", got.HasStagedIntent, tc.want)
			}
		})
	}
}

func TestReadDetectsStagedExtras(t *testing.T) {
	cases := []struct {
		name  string
		field string
		want  bool
	}{
		{name: "non-empty", field: `"stagedExtras":[{}],`, want: true},
		{name: "empty", field: `"stagedExtras":[],`},
		{name: "null", field: `"stagedExtras":null,`},
		{name: "absent"},
		{name: "title case", field: `"StagedExtras":[{}],`, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSeriesJSON(
				t,
				`{"schemaVersion":3,`+tc.field+`"metadataRef":"tvdb:370070"}`,
			)
			got, err := seriesmeta.Read(path)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if got.HasStagedIntent != tc.want {
				t.Fatalf("HasStagedIntent = %t, want %t", got.HasStagedIntent, tc.want)
			}
		})
	}
}

func TestReadDetectsActiveClaim(t *testing.T) {
	cases := []struct {
		name  string
		field string
		want  bool
	}{
		{name: "object", field: `"in_progress":{},`, want: true},
		{name: "null", field: `"in_progress":null,`},
		{name: "absent"},
		{name: "title case", field: `"In_Progress":{},`, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSeriesJSON(
				t,
				`{"schemaVersion":3,`+tc.field+`"metadataRef":"tvdb:370070"}`,
			)
			got, err := seriesmeta.Read(path)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if got.HasActiveClaim != tc.want {
				t.Fatalf("HasActiveClaim = %t, want %t", got.HasActiveClaim, tc.want)
			}
		})
	}
}

func TestReadRejectsUnsupportedSchemaVersion(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{
			name: "pre-v3",
			data: `{"schemaVersion":2}`,
			want: "seriesmeta: unsupported schemaVersion 2",
		},
		{
			name: "post-v3",
			data: `{"schemaVersion":4}`,
			want: "seriesmeta: unsupported schemaVersion 4",
		},
		{
			name: "absent",
			data: `{}`,
			want: "seriesmeta: unsupported schemaVersion 0",
		},
		{
			name: "null",
			data: `{"schemaVersion":null}`,
			want: "seriesmeta: unsupported schemaVersion 0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := seriesmeta.Read(writeSeriesJSON(t, tc.data))
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Read error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestReadRejectsUnsupportedSchemaVersionBeforeBody(t *testing.T) {
	path := writeSeriesJSON(t, `{"schemaVersion":4,"stagedTrash":{"a":1}}`)
	_, err := seriesmeta.Read(path)
	const want = "seriesmeta: unsupported schemaVersion 4"
	if err == nil || err.Error() != want {
		t.Fatalf("Read error = %v, want %q", err, want)
	}
}

func TestReadRejectsInvalidUTF8(t *testing.T) {
	data := []byte(`{"schemaVersion":3,"metadataRef":"tvdb:`)
	data = append(data, 0xff)
	data = append(data, []byte(`"}`)...)

	_, err := seriesmeta.Read(writeSeriesBytes(t, data))
	const want = "seriesmeta: series.json must be valid UTF-8"
	if err == nil || err.Error() != want {
		t.Fatalf("Read error = %v, want %q", err, want)
	}
}

func TestReadRejectsMalformedJSON(t *testing.T) {
	_, err := seriesmeta.Read(writeSeriesJSON(t, `{"schemaVersion":3`))
	const want = "seriesmeta: decode: unexpected end of JSON input"
	if err == nil || err.Error() != want {
		t.Fatalf("Read error = %v, want %q", err, want)
	}
}

func TestReadWrapsFileError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	_, err := seriesmeta.Read(path)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Read error = %v, want os.ErrNotExist", err)
	}
}

func writeSeriesJSON(t *testing.T, data string) string {
	t.Helper()
	return writeSeriesBytes(t, []byte(data))
}

func writeSeriesBytes(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "series.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}
