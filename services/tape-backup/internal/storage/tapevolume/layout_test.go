package tapevolume_test

import (
	"math/rand"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
)

func TestLayoutPaths(t *testing.T) {
	root := filepath.Join("mnt", "ltfs")
	if got, want := tapevolume.ArchiveDir(root), filepath.Join(root, "KURA_ARCHIVE"); got != want {
		t.Fatalf("ArchiveDir = %q, want %q", got, want)
	}
	if got, want := tapevolume.VolumeFile(root), filepath.Join(root, "KURA_ARCHIVE", "volume.json"); got != want {
		t.Fatalf("VolumeFile = %q, want %q", got, want)
	}
	if got, want := tapevolume.SnapshotsDir(root), filepath.Join(root, "KURA_ARCHIVE", "snapshots"); got != want {
		t.Fatalf("SnapshotsDir = %q, want %q", got, want)
	}

	name, err := tapevolume.SnapshotName("tvdb:370070", 7)
	if err != nil {
		t.Fatalf("SnapshotName: %v", err)
	}
	if want := "OR3GIYR2GM3TAMBXGA.g7"; name != want {
		t.Fatalf("SnapshotName = %q, want %q", name, want)
	}
	got, err := tapevolume.SnapshotDir(root, "tvdb:370070", 7)
	if err != nil {
		t.Fatalf("SnapshotDir: %v", err)
	}
	want := filepath.Join(root, "KURA_ARCHIVE", "snapshots", name)
	if got != want {
		t.Fatalf("SnapshotDir = %q, want %q", got, want)
	}
}

func TestSnapshotNameRoundTrip(t *testing.T) {
	cases := []struct {
		name        string
		metadataRef string
		generation  int
	}{
		{name: "plain TVDB", metadataRef: "tvdb:370070", generation: 1},
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
			name, err := tapevolume.SnapshotName(tc.metadataRef, tc.generation)
			if err != nil {
				t.Fatalf("SnapshotName: %v", err)
			}
			gotRef, gotGeneration, err := tapevolume.ParseSnapshotName(name)
			if err != nil {
				t.Fatalf("ParseSnapshotName: %v", err)
			}
			if gotRef != tc.metadataRef || gotGeneration != tc.generation {
				t.Fatalf(
					"ParseSnapshotName = (%q, %d), want (%q, %d)",
					gotRef,
					gotGeneration,
					tc.metadataRef,
					tc.generation,
				)
			}
		})
	}
}

func TestSnapshotNameRandomizedRoundTrip(t *testing.T) {
	random := rand.New(rand.NewSource(370070))
	for range 1_000 {
		refBytes := make([]byte, random.Intn(128)+1)
		if _, err := random.Read(refBytes); err != nil {
			t.Fatalf("Read random bytes: %v", err)
		}
		metadataRef := string(refBytes)
		generation := random.Intn(1_000_000) + 1

		name, err := tapevolume.SnapshotName(metadataRef, generation)
		if err != nil {
			t.Fatalf("SnapshotName: %v", err)
		}
		gotRef, gotGeneration, err := tapevolume.ParseSnapshotName(name)
		if err != nil {
			t.Fatalf("ParseSnapshotName(%q): %v", name, err)
		}
		if gotRef != metadataRef || gotGeneration != generation {
			t.Fatalf(
				"ParseSnapshotName = (%q, %d), want (%q, %d)",
				gotRef,
				gotGeneration,
				metadataRef,
				generation,
			)
		}
	}
}

func TestSnapshotNameInjective(t *testing.T) {
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
		name, err := tapevolume.SnapshotName(pair.metadataRef, pair.generation)
		if err != nil {
			t.Fatalf("SnapshotName(%q, %d): %v", pair.metadataRef, pair.generation, err)
		}
		if _, ok := seen[name]; ok {
			t.Fatalf(
				"SnapshotName collision for (%q, %d): %q",
				pair.metadataRef,
				pair.generation,
				name,
			)
		}
		seen[name] = struct{}{}
	}
}

func TestSnapshotNameValidation(t *testing.T) {
	cases := []struct {
		name        string
		metadataRef string
		generation  int
		want        string
	}{
		{
			name:        "metadata ref required",
			metadataRef: "",
			generation:  1,
			want:        "tapevolume: metadataRef is required",
		},
		{
			name:        "generation positive",
			metadataRef: "tvdb:370070",
			generation:  0,
			want:        "tapevolume: generation must be at least 1",
		},
		{
			name:        "snapshot name length",
			metadataRef: strings.Repeat("provider:series/part.one-two_three ", 200),
			generation:  999_999,
			want:        "tapevolume: snapshot name exceeds 255-byte limit",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tapevolume.SnapshotName(tc.metadataRef, tc.generation)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("SnapshotName error = %v, want %q", err, tc.want)
			}
			_, err = tapevolume.SnapshotDir("root", tc.metadataRef, tc.generation)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("SnapshotDir error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestParseSnapshotNameRejectsNonCanonicalGeneration(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "leading plus",
			input: "MY.g+7",
			want:  `tapevolume: snapshot name "MY.g+7" has invalid generation "+7"`,
		},
		{
			name:  "one leading zero",
			input: "MY.g07",
			want:  `tapevolume: snapshot name "MY.g07" has invalid generation "07"`,
		},
		{
			name:  "two leading zeros",
			input: "MY.g007",
			want:  `tapevolume: snapshot name "MY.g007" has invalid generation "007"`,
		},
		{
			name:  "leading whitespace",
			input: "MY.g 1",
			want:  `tapevolume: snapshot name "MY.g 1" has invalid generation " 1"`,
		},
		{
			name:  "empty",
			input: "MY.g",
			want:  `tapevolume: snapshot name "MY.g" has invalid generation ""`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tapevolume.ParseSnapshotName(tc.input)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("ParseSnapshotName error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestParseSnapshotNameRequiresMetadataRef(t *testing.T) {
	_, _, err := tapevolume.ParseSnapshotName("A.g1")
	want := "tapevolume: metadataRef is required"
	if err == nil || err.Error() != want {
		t.Fatalf("ParseSnapshotName error = %v, want %q", err, want)
	}
}

func TestParseSnapshotNameRejectsInvalid(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "missing separator",
			input: "MZXW6",
			want:  `tapevolume: snapshot name "MZXW6" is missing .g<generation>`,
		},
		{
			name:  "g0 without separator",
			input: "g0",
			want:  `tapevolume: snapshot name "g0" is missing .g<generation>`,
		},
		{
			name:  "zero generation",
			input: "MY.g0",
			want:  `tapevolume: snapshot name "MY.g0" generation must be at least 1`,
		},
		{
			name:  "negative generation",
			input: "MY.g-1",
			want:  `tapevolume: snapshot name "MY.g-1" has invalid generation "-1"`,
		},
		{
			name:  "non-numeric generation",
			input: "MY.gnope",
			want:  `tapevolume: snapshot name "MY.gnope" has invalid generation "nope"`,
		},
		{
			name:  "invalid base32",
			input: "INVALID!.g1",
			want:  `tapevolume: snapshot name "INVALID!.g1" has invalid base32 metadataRef: illegal base32 data at input byte 7`,
		},
		{
			name:  "lowercase base32",
			input: "my.g1",
			want:  `tapevolume: snapshot name "my.g1" has invalid base32 metadataRef: illegal base32 data at input byte 0`,
		},
		{
			name:  "non-canonical base32",
			input: "MZ.g1",
			want:  `tapevolume: snapshot name "MZ.g1" is not canonical`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tapevolume.ParseSnapshotName(tc.input)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("ParseSnapshotName error = %v, want %q", err, tc.want)
			}
		})
	}
}
