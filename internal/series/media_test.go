package series

import (
	"testing"

	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
)

func TestFileTitleNormalizesAndCompares(t *testing.T) {
	title, err := ParseFileTitle(" 本好きの下剋上 司書になるためには手段を選んでいられません ")
	if err != nil {
		t.Fatalf("ParseFileTitle: %v", err)
	}
	if !title.EqualName("本好きの下剋上 司書になるためには手段を選んでいられません") {
		t.Fatal("EqualName = false, want NFC-equivalent title match")
	}
	if _, err := ParseFileTitle("Bad/Title"); err == nil {
		t.Fatal("ParseFileTitle returned nil error, want separator rejection")
	}
}

func TestBuildMediaFilename(t *testing.T) {
	title, err := ParseFileTitle("Bookworm")
	if err != nil {
		t.Fatalf("ParseFileTitle: %v", err)
	}
	episode, _ := refs.NewEpisode(1, 1)
	resolution, _ := media.ParseResolution("1920x1080")
	filename := BuildMediaFilename(title, episode, MediaFilenameFacts{
		Source:     media.ParseSource("webrip"),
		Resolution: resolution,
	}, ".mkv")
	if filename.String() != "Bookworm - S01E01 (WebRip 1080p).mkv" {
		t.Fatalf("filename = %q", filename.String())
	}
}
