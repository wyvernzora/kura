package series

import (
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/refs"
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
				AirDate: "2019-10-02",
				Active: &MediaRecord{
					Path:       "Season 1/Honzuki - S01E01 (BDRip 1080p).mkv",
					Source:     "BDRip",
					Resolution: "1080p",
					Codec:      "H.264",
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
	if got := out.Episodes[episodeRef].AirDate; got != "2019-10-02" {
		t.Fatalf("air date = %q", got)
	}
	if out.Episodes[episodeRef].Active == nil {
		t.Fatal("active record missing")
	}
}

func TestEditorRefreshSpineNeverRemovesEpisodes(t *testing.T) {
	oldRef, _ := refs.NewEpisode(1, 1)
	newRef, _ := refs.NewEpisode(1, 2)
	series := seriesState{
		Metadata: refs.Metadata("tvdb:370070"),
		Episodes: map[refs.Episode]episodeState{
			oldRef: {AirDate: "2019-10-02"},
		},
	}
	editor{series: &series}.refreshSpine([]SpineEpisode{{Ref: newRef, AirDate: "2019-10-09"}})
	if _, ok := series.Episodes[oldRef]; !ok {
		t.Fatal("refreshSpine removed old spine entry")
	}
	if got := series.Episodes[newRef].AirDate; got != "2019-10-09" {
		t.Fatalf("new air date = %q", got)
	}
}
