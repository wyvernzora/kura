package series

import (
	"testing"
	"time"

	"cloud.google.com/go/civil"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/series/wire"
)

func TestWireRoundTrip(t *testing.T) {
	episodeRef, err := refs.NewEpisode(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	mtime := time.Date(2024, 3, 15, 10, 0, 0, 0, time.UTC)
	in := seriesState{
		Metadata:    refs.Metadata("tvdb:370070"),
		LastScanned: mtime,
		Episodes: map[refs.Episode]episodeState{
			episodeRef: {
				AirDate: mustParseDate(t, "2019-10-02"),
				Active: &MediaRecord{
					Path:       "Season 1/Honzuki - S01E01 (BDRip 1080p).mkv",
					Source:     media.SourceBDRip,
					Resolution: mustParseResolution(t, "1920x1080"),
					Codec:      media.Codec("H.264"),
					Size:       123,
					MTime:      mtime,
					Companions: []CompanionRecord{},
				},
			},
		},
	}
	encoded, err := toWire(in)
	if err != nil {
		t.Fatal(err)
	}
	data, err := wire.Encode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := wire.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	out, err := fromWire(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if out.Metadata != in.Metadata {
		t.Fatalf("metadata = %s, want %s", out.Metadata, in.Metadata)
	}
	if got := out.Episodes[episodeRef].AirDate.String(); got != "2019-10-02" {
		t.Fatalf("air date = %q", got)
	}
	if out.Episodes[episodeRef].Active == nil {
		t.Fatal("active record missing")
	}
}

func mustParseResolution(t *testing.T, value string) media.Resolution {
	t.Helper()
	r, err := media.ParseResolution(value)
	if err != nil {
		t.Fatalf("ParseResolution(%q): %v", value, err)
	}
	return r
}

func mustParseDate(t *testing.T, value string) civil.Date {
	t.Helper()
	d, err := civil.ParseDate(value)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", value, err)
	}
	return d
}

func TestEditorRefreshSpineNeverRemovesEpisodes(t *testing.T) {
	oldRef, _ := refs.NewEpisode(1, 1)
	newRef, _ := refs.NewEpisode(1, 2)
	series := seriesState{
		Metadata: refs.Metadata("tvdb:370070"),
		Episodes: map[refs.Episode]episodeState{
			oldRef: {AirDate: mustParseDate(t, "2019-10-02")},
		},
	}
	editor{series: &series}.refreshSpine([]SpineEpisode{{Ref: newRef, AirDate: mustParseDate(t, "2019-10-09")}})
	if _, ok := series.Episodes[oldRef]; !ok {
		t.Fatal("refreshSpine removed old spine entry")
	}
	if got := series.Episodes[newRef].AirDate.String(); got != "2019-10-09" {
		t.Fatalf("new air date = %q", got)
	}
}
