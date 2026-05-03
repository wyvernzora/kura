package mcp

import (
	"errors"
	"testing"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/workflow"
)

func TestResolveSeriesRef_HitReturnsSeries(t *testing.T) {
	idx := indexfile.New(t.TempDir())
	want := mustSeries(t, "Bookworm")
	if err := idx.Put(refs.Metadata("tvdb:1"), want); err != nil {
		t.Fatal(err)
	}
	got, err := resolveSeriesRef(idx, refs.Metadata("tvdb:1"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != want {
		t.Fatalf("got = %v, want %v", got, want)
	}
}

func TestResolveSeriesRef_MissReturnsNotIndexedError(t *testing.T) {
	idx := indexfile.New(t.TempDir())
	_, err := resolveSeriesRef(idx, refs.Metadata("tvdb:404"))
	var notIdx *workflow.MetadataRefNotIndexedError
	if !errors.As(err, &notIdx) {
		t.Fatalf("err = %v, want *MetadataRefNotIndexedError", err)
	}
	if notIdx.Ref.String() != "tvdb:404" {
		t.Fatalf("notIdx.Ref = %v, want tvdb:404", notIdx.Ref)
	}
}
