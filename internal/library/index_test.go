package library

import (
	"testing"

	"github.com/wyvernzora/kura/internal/refs"
)

func mustSeries(t *testing.T, value string) refs.Series {
	t.Helper()
	ref, err := refs.ParseSeries(value)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestIndexSaveLoad(t *testing.T) {
	root, err := ParseRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	idx := NewIndex(root)
	honzuki := mustSeries(t, "Honzuki")
	if err := idx.Put(refs.Metadata("tvdb:370070"), honzuki); err != nil {
		t.Fatal(err)
	}
	if err := idx.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := loaded.Get(refs.Metadata("tvdb:370070"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != honzuki {
		t.Fatalf("index get = %q, %v", got, ok)
	}
}

func TestIndexRejectsDuplicateMetadataRef(t *testing.T) {
	root, err := ParseRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	idx := NewIndex(root)
	if err := idx.Put(refs.Metadata("tvdb:370070"), mustSeries(t, "A")); err != nil {
		t.Fatal(err)
	}
	if err := idx.Put(refs.Metadata("tvdb:370070"), mustSeries(t, "B")); err == nil {
		t.Fatal("expected duplicate ref error")
	}
}
