package filename_test

import (
	"testing"

	"github.com/wyvernzora/kura/internal/domain/filename"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
)

func TestParseTitleRejectsEmpty(t *testing.T) {
	if _, err := filename.ParseTitle("   "); err == nil {
		t.Fatal("ParseTitle: nil err for whitespace title")
	}
}

func TestParseTitleRejectsSeparators(t *testing.T) {
	if _, err := filename.ParseTitle("foo/bar"); err == nil {
		t.Fatal("ParseTitle: nil err for slash title")
	}
	if _, err := filename.ParseTitle("foo\\bar"); err == nil {
		t.Fatal("ParseTitle: nil err for backslash title")
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
