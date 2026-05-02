package library

import (
	"context"
	"os"
	"testing"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/progress"
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

func TestIndexRemove(t *testing.T) {
	root, err := ParseRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	idx := NewIndex(root)
	bookworm := mustSeries(t, "Bookworm")
	other := mustSeries(t, "Other")
	if err := idx.Put(refs.Metadata("tvdb:370070"), bookworm); err != nil {
		t.Fatal(err)
	}
	if err := idx.Put(refs.Metadata("tvdb:999999"), bookworm); err != nil {
		t.Fatal(err)
	}
	if err := idx.Put(refs.Metadata("tvdb:111111"), other); err != nil {
		t.Fatal(err)
	}
	idx.Remove(bookworm)
	if _, ok, err := idx.Get(refs.Metadata("tvdb:370070")); err != nil || ok {
		t.Fatalf("Get old ref = _, %v, %v; want absent", ok, err)
	}
	if _, ok, err := idx.Get(refs.Metadata("tvdb:999999")); err != nil || ok {
		t.Fatalf("Get second old ref = _, %v, %v; want absent", ok, err)
	}
	if got, ok, err := idx.Get(refs.Metadata("tvdb:111111")); err != nil || !ok || got != other {
		t.Fatalf("Get other = %q, %v, %v; want %q, true, nil", got, ok, err, other)
	}
}

func TestRebuildIndexReportsProgress(t *testing.T) {
	root, err := ParseRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root.Join("Bookworm"), 0o755); err != nil {
		t.Fatal(err)
	}
	var events []progress.Event
	ctx := progress.With(context.Background(), func(_ context.Context, event progress.Event) {
		events = append(events, event)
	})
	_, err = RebuildIndex(ctx, root, func(context.Context, refs.Series) (refs.Metadata, error) {
		return refs.Metadata("tvdb:370070"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 3 {
		t.Fatalf("events = %#v, want start/update/success", events)
	}
	if events[0].Status != progress.StartStatus || events[0].Stage != "reindex" {
		t.Fatalf("first event = %#v, want reindex start", events[0])
	}
	if events[len(events)-1].Status != progress.SuccessStatus || events[len(events)-1].Stage != "reindex" {
		t.Fatalf("last event = %#v, want reindex success", events[len(events)-1])
	}
}
