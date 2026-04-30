package index

import (
	"testing"

	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/refs"
)

func TestIndexSaveLoad(t *testing.T) {
	root, err := fsroot.ParseLibraryRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	idx := New(root)
	if err := idx.Put(refs.Metadata("tvdb:370070"), refs.Series("Honzuki")); err != nil {
		t.Fatal(err)
	}
	if err := idx.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := loaded.Get(refs.Metadata("tvdb:370070"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != refs.Series("Honzuki") {
		t.Fatalf("index get = %q, %v", got, ok)
	}
}

func TestIndexRejectsDuplicateMetadataRef(t *testing.T) {
	root, err := fsroot.ParseLibraryRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	idx := New(root)
	if err := idx.Put(refs.Metadata("tvdb:370070"), refs.Series("A")); err != nil {
		t.Fatal(err)
	}
	if err := idx.Put(refs.Metadata("tvdb:370070"), refs.Series("B")); err == nil {
		t.Fatal("expected duplicate ref error")
	}
}
