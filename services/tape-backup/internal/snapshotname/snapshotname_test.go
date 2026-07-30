package snapshotname_test

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/wyvernzora/kura/services/tape-backup/internal/snapshotname"
)

func TestFormatParseRoundTrip(t *testing.T) {
	cases := []struct {
		name        string
		metadataRef string
		generation  int
		wantName    string
	}{
		{
			name:        "plain TVDB",
			metadataRef: "tvdb:370070",
			generation:  7,
			wantName:    "OR3GIYR2GM3TAMBXGA.g7",
		},
		{name: "hyphen", metadataRef: "provider:a-b", generation: 2},
		{name: "underscore", metadataRef: "provider:a_b", generation: 3},
		{name: "dot", metadataRef: "provider:a.b", generation: 4},
		{name: "repeated colon", metadataRef: "provider:::value", generation: 5},
		{name: "slash", metadataRef: "provider:a/b", generation: 6},
		{name: "space", metadataRef: "provider:a b", generation: 7},
		{name: "unicode", metadataRef: "anilist:葬送のフリーレン", generation: 8},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name, err := snapshotname.Format(tc.metadataRef, tc.generation)
			if err != nil {
				t.Fatalf("Format: %v", err)
			}
			if tc.wantName != "" && name != tc.wantName {
				t.Fatalf("Format = %q, want %q", name, tc.wantName)
			}
			gotRef, gotGeneration, err := snapshotname.Parse(name)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if gotRef != tc.metadataRef || gotGeneration != tc.generation {
				t.Fatalf(
					"Parse = (%q, %d), want (%q, %d)",
					gotRef,
					gotGeneration,
					tc.metadataRef,
					tc.generation,
				)
			}
		})
	}
}

func TestFormatParseRandomizedRoundTrip(t *testing.T) {
	random := rand.New(rand.NewSource(370070))
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789:/._- "
	for range 1_000 {
		refBytes := make([]byte, random.Intn(128)+1)
		for i := range refBytes {
			refBytes[i] = alphabet[random.Intn(len(alphabet))]
		}
		metadataRef := string(refBytes)
		generation := random.Intn(1_000_000) + 1

		name, err := snapshotname.Format(metadataRef, generation)
		if err != nil {
			t.Fatalf("Format: %v", err)
		}
		gotRef, gotGeneration, err := snapshotname.Parse(name)
		if err != nil {
			t.Fatalf("Parse(%q): %v", name, err)
		}
		if gotRef != metadataRef || gotGeneration != generation {
			t.Fatalf(
				"Parse = (%q, %d), want (%q, %d)",
				gotRef,
				gotGeneration,
				metadataRef,
				generation,
			)
		}
	}
}

func TestFormatIsInjective(t *testing.T) {
	pairs := []struct {
		metadataRef string
		generation  int
	}{
		{metadataRef: "a:b-c", generation: 1},
		{metadataRef: "a-b:c", generation: 1},
		{metadataRef: "a:b-c", generation: 2},
		{metadataRef: "a:b.c", generation: 1},
		{metadataRef: "a:b_c", generation: 1},
		{metadataRef: "a::b", generation: 1},
		{metadataRef: "a/b", generation: 1},
		{metadataRef: "a b", generation: 1},
	}

	seen := make(map[string]struct{}, len(pairs))
	for _, pair := range pairs {
		name, err := snapshotname.Format(pair.metadataRef, pair.generation)
		if err != nil {
			t.Fatalf("Format(%q, %d): %v", pair.metadataRef, pair.generation, err)
		}
		if _, ok := seen[name]; ok {
			t.Fatalf(
				"Format collision for (%q, %d): %q",
				pair.metadataRef,
				pair.generation,
				name,
			)
		}
		seen[name] = struct{}{}
	}
}

func TestFormatValidation(t *testing.T) {
	cases := []struct {
		name        string
		metadataRef string
		generation  int
		want        string
	}{
		{
			name:       "metadata ref required",
			generation: 1,
			want:       "metadataRef is required",
		},
		{
			name:        "metadata ref valid UTF-8",
			metadataRef: "\xff",
			generation:  1,
			want:        "metadataRef must be valid UTF-8",
		},
		{
			name:        "generation positive",
			metadataRef: "tvdb:370070",
			generation:  0,
			want:        "generation must be at least 1",
		},
		{
			name:        "snapshot name length",
			metadataRef: strings.Repeat("provider:series/part.one-two_three ", 200),
			generation:  999_999,
			want:        "snapshot name exceeds 255-byte limit",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := snapshotname.Format(tc.metadataRef, tc.generation)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Format error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestParseRejectsNonCanonicalGeneration(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "leading plus",
			input: "MY.g+7",
			want:  `snapshot name "MY.g+7" has invalid generation "+7"`,
		},
		{
			name:  "one leading zero",
			input: "MY.g07",
			want:  `snapshot name "MY.g07" has invalid generation "07"`,
		},
		{
			name:  "two leading zeros",
			input: "MY.g007",
			want:  `snapshot name "MY.g007" has invalid generation "007"`,
		},
		{
			name:  "leading whitespace",
			input: "MY.g 1",
			want:  `snapshot name "MY.g 1" has invalid generation " 1"`,
		},
		{
			name:  "empty",
			input: "MY.g",
			want:  `snapshot name "MY.g" has invalid generation ""`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := snapshotname.Parse(tc.input)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Parse error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestParseValidation(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "snapshot name length",
			input: strings.Repeat("A", 254) + ".g1",
			want:  "snapshot name exceeds 255-byte limit",
		},
		{
			name:  "missing separator",
			input: "MZXW6",
			want:  `snapshot name "MZXW6" is missing .g<generation>`,
		},
		{
			name:  "wrong generation delimiter",
			input: "MZXW6.x7",
			want:  `snapshot name "MZXW6.x7" is missing .g<generation>`,
		},
		{
			name:  "zero generation",
			input: "MY.g0",
			want:  `snapshot name "MY.g0" generation must be at least 1`,
		},
		{
			name:  "negative generation",
			input: "MY.g-1",
			want:  `snapshot name "MY.g-1" has invalid generation "-1"`,
		},
		{
			name:  "non-numeric generation",
			input: "MY.gnope",
			want:  `snapshot name "MY.gnope" has invalid generation "nope"`,
		},
		{
			name:  "invalid base32",
			input: "INVALID!.g1",
			want: `snapshot name "INVALID!.g1" has invalid base32 metadataRef: ` +
				`illegal base32 data at input byte 7`,
		},
		{
			name:  "lowercase base32",
			input: "my.g1",
			want: `snapshot name "my.g1" has invalid base32 metadataRef: ` +
				`illegal base32 data at input byte 0`,
		},
		{
			name:  "non-canonical base32",
			input: "MZ.g1",
			want:  `snapshot name "MZ.g1" is not canonical`,
		},
		{
			name:  "metadata ref required",
			input: "A.g1",
			want:  "metadataRef is required",
		},
		{
			name:  "metadata ref valid UTF-8",
			input: "74.g1",
			want:  "metadataRef must be valid UTF-8",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := snapshotname.Parse(tc.input)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Parse error = %v, want %q", err, tc.want)
			}
		})
	}
}
