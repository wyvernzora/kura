package indexfile_test

import (
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/textnorm"
)

func mustParseSeries(t *testing.T, value string) refs.Series {
	t.Helper()
	ref, err := refs.ParseSeries(value)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func minimalModel(t *testing.T, ref string, metadata refs.Metadata) *series.Series {
	t.Helper()
	seriesRef := mustParseSeries(t, ref)
	return &series.Series{
		Ref:            seriesRef,
		Metadata:       metadata,
		PreferredTitle: textnorm.NFC(ref),
		Episodes:       map[refs.Episode]series.Episode{},
		LastMutated:    coord.Mutator{Op: "test", PID: 1, Host: "test", At: time.Unix(0, 0).UTC()},
	}
}
