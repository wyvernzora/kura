package series

import (
	"testing"

	"cloud.google.com/go/civil"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
)

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

func TestRefreshSpineNeverRemovesEpisodes(t *testing.T) {
	oldRef, _ := refs.NewEpisode(1, 1)
	newRef, _ := refs.NewEpisode(1, 2)
	model := seriesState{
		Metadata: refs.Metadata("tvdb:370070"),
		Episodes: map[refs.Episode]episodeState{
			oldRef: {AirDate: mustParseDate(t, "2019-10-02")},
		},
	}
	model.RefreshSpine([]SpineEpisode{{Ref: newRef, AirDate: mustParseDate(t, "2019-10-09")}})
	if _, ok := model.Episodes[oldRef]; !ok {
		t.Fatal("RefreshSpine removed old spine entry")
	}
	if got := model.Episodes[newRef].AirDate.String(); got != "2019-10-09" {
		t.Fatalf("new air date = %q", got)
	}
}
