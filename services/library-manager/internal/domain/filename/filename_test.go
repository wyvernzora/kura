package filename_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/filename"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/media"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
)

func utf8ValidMedia(m filename.Media) bool {
	return utf8.ValidString(string(m))
}

func TestParseTitleRejectsEmpty(t *testing.T) {
	if _, err := filename.ParseTitle("   "); err == nil {
		t.Fatal("ParseTitle: nil err for whitespace title")
	}
	if _, err := filename.ParseTitle("..."); err == nil {
		t.Fatal("ParseTitle: nil err for dots-only title")
	}
	// "?" sanitizes to space which is then trimmed to empty.
	if _, err := filename.ParseTitle("?"); err == nil {
		t.Fatal("ParseTitle: nil err for question-mark-only title")
	}
}

func TestParseTitleSanitizesSeparators(t *testing.T) {
	got, err := filename.ParseTitle("foo/bar")
	if err != nil {
		t.Fatalf("ParseTitle(foo/bar): %v", err)
	}
	if got.String() != "foo bar" {
		t.Errorf("foo/bar -> %q, want \"foo bar\"", got.String())
	}
	got, err = filename.ParseTitle("foo\\bar")
	if err != nil {
		t.Fatalf("ParseTitle(foo\\bar): %v", err)
	}
	if got.String() != "foo bar" {
		t.Errorf("foo\\bar -> %q, want \"foo bar\"", got.String())
	}
}

func TestSanitize(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Pretty Cure: The Movie", "Pretty Cure - The Movie"},
		{"What?!", "What !"},
		{"<bracket>", "bracket"},
		{`"quoted"`, "quoted"},
		{"a|b", "a b"},
		{"a*b", "a b"},
		{"foo  bar   baz", "foo bar baz"},
		{"  leading and trailing  ", "leading and trailing"},
		{"trailing dots...", "trailing dots"},
		{".leading dot", "leading dot"},
		{"foo\x00bar", "foobar"}, // control strip
		{"foo\tbar", "foo bar"},  // tab collapses to space
		{"a b", "a b"},           // NBSP collapses
		{"Foo: Bar / Baz", "Foo - Bar Baz"},
		{"", ""},
		{".  .  .", ""},
	}
	for _, tc := range cases {
		got := filename.Sanitize(tc.in)
		if got != tc.want {
			t.Errorf("Sanitize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildMediaCapsAt255Bytes(t *testing.T) {
	long := strings.Repeat("a", 300)
	title := filename.CleanTitle(long)
	episode, _ := refs.NewEpisode(1, 1)
	got := filename.BuildMedia(title, episode, filename.Facts{
		Source: media.SourceUnknown,
	}, ".mkv")
	if len(got) > filename.MaxBasenameBytes {
		t.Fatalf("len = %d, want <= %d", len(got), filename.MaxBasenameBytes)
	}
	// Suffix must be preserved verbatim.
	if !strings.HasSuffix(string(got), " - S01E01 (Unknown UnknownResolution).mkv") {
		t.Fatalf("suffix lost: %q", got)
	}
}

func TestBuildMediaCapPreservesUTF8RuneBoundary(t *testing.T) {
	// Multibyte runes; truncation must not split mid-rune.
	long := strings.Repeat("漫", 100) // 3 bytes per rune × 100 = 300 bytes
	title := filename.CleanTitle(long)
	episode, _ := refs.NewEpisode(1, 1)
	got := filename.BuildMedia(title, episode, filename.Facts{
		Source: media.SourceUnknown,
	}, ".mkv")
	if len(got) > filename.MaxBasenameBytes {
		t.Fatalf("len = %d, want <= %d", len(got), filename.MaxBasenameBytes)
	}
	if !utf8ValidMedia(got) {
		t.Fatalf("invalid UTF-8 after cap: %q", got)
	}
}

func TestBuildMediaCanonical(t *testing.T) {
	title, err := filename.ParseTitle("Bookworm")
	if err != nil {
		t.Fatal(err)
	}
	episode, err := refs.NewEpisode(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := media.ParseResolution("1920x1080")
	if err != nil {
		t.Fatal(err)
	}
	got := filename.BuildMedia(title, episode, filename.Facts{
		Source:     media.SourceWebRip,
		Resolution: resolution,
	}, ".mkv")
	want := filename.Media("Bookworm - S01E01 (WebRip 1080p).mkv")
	if got != want {
		t.Fatalf("BuildMedia = %q, want %q", got, want)
	}
}

func TestBuildMediaUnknownResolutionPlaceholder(t *testing.T) {
	title := filename.CleanTitle("Foo")
	episode, _ := refs.NewEpisode(2, 3)
	got := filename.BuildMedia(title, episode, filename.Facts{
		Source: media.SourceUnknown,
	}, ".mkv")
	want := filename.Media("Foo - S02E03 (Unknown UnknownResolution).mkv")
	if got != want {
		t.Fatalf("BuildMedia = %q, want %q", got, want)
	}
}
