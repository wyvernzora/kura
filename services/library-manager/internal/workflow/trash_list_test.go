package workflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/wyvernzora/kura/services/library-manager/internal/coord"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/series"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/indexfile"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/trashfile"
	"github.com/wyvernzora/kura/services/library-manager/internal/textnorm"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
)

// writeTrashEntry drops one trash bucket under <libRoot>/<ref>/.kura/trash.
func writeTrashEntry(t *testing.T, libRoot string, ref refs.Series) {
	t.Helper()
	ep, err := refs.NewEpisode(1, 1)
	if err != nil {
		t.Fatalf("NewEpisode: %v", err)
	}
	meta := trashfile.Meta{
		ID:        ulid.Make(),
		Episode:   ep,
		TrashedAt: time.Now().Add(-24 * time.Hour),
		Record: trashfile.Record{
			Path: "Season 1/" + ref.String() + " - S01E01.mkv",
			Size: 10,
		},
	}
	if err := trashfile.Write(libRoot, ref, meta); err != nil {
		t.Fatalf("trashfile.Write(%s): %v", ref, err)
	}
}

// trashFixture builds a library holding two directories with trash —
// one indexed ("Indexed", tvdb:4242), one the index has never seen
// (the returned orphan).
func trashFixture(t *testing.T) (deps workflow.Deps, orphan refs.Series) {
	t.Helper()
	libRoot := t.TempDir()
	idx := indexfile.New(libRoot, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})

	indexed := mustParseSeries(t, "Indexed")
	orphan = mustParseSeries(t, "Orphan")
	if err := idx.Upsert(indexfile.Entry{Model: &series.Series{
		Ref:            indexed,
		Metadata:       refs.Metadata("tvdb:4242"),
		PreferredTitle: textnorm.NFC("Indexed"),
		LastMutated:    coord.Mutator{Op: "test", PID: 1, Host: "test", At: time.Unix(0, 0).UTC()},
	}}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	writeTrashEntry(t, libRoot, indexed)
	writeTrashEntry(t, libRoot, orphan)

	return workflow.Deps{
		LibRoot:     libRoot,
		Index:       idx,
		Coordinator: coord.NewMCPCoordinator(),
		Now:         time.Now,
	}, orphan
}

// TestTrashList_AllSkipsUnindexedDirectories pins the scope rule: `--all`
// enumerates the index, so a directory holding `.kura/trash` content
// that kura does not track is invisible to trash management.
func TestTrashList_AllSkipsUnindexedDirectories(t *testing.T) {
	deps, _ := trashFixture(t)

	out, err := workflow.TrashList(context.Background(), deps, workflow.TrashListInput{All: true})
	if err != nil {
		t.Fatalf("TrashList: %v", err)
	}
	if len(out.Series) != 1 {
		t.Fatalf("series: got %d want 1 (%+v)", len(out.Series), out.Series)
	}
	if got := out.Series[0].Directory.String(); got != "Indexed" {
		t.Errorf("directory: got %q want %q", got, "Indexed")
	}
}

// TestTrashList_CarriesMetadataRef pins the field the web trash page
// uses to address the per-series routes.
func TestTrashList_CarriesMetadataRef(t *testing.T) {
	deps, _ := trashFixture(t)

	out, err := workflow.TrashList(context.Background(), deps, workflow.TrashListInput{All: true})
	if err != nil {
		t.Fatalf("TrashList: %v", err)
	}
	if len(out.Series) != 1 {
		t.Fatalf("series: got %d want 1 (%+v)", len(out.Series), out.Series)
	}
	if got := out.Series[0].Ref; got != refs.Metadata("tvdb:4242") {
		t.Errorf("ref: got %q want %q", got, "tvdb:4242")
	}
}

// TestTrashEmpty_AllSkipsUnindexedDirectories is the destructive half of
// the scope rule: an unindexed directory's trash survives `--all`.
func TestTrashEmpty_AllSkipsUnindexedDirectories(t *testing.T) {
	deps, orphan := trashFixture(t)

	out, err := workflow.TrashEmpty(context.Background(), deps, workflow.TrashEmptyInput{All: true})
	if err != nil {
		t.Fatalf("TrashEmpty: %v", err)
	}
	if out.TotalEntries != 1 {
		t.Errorf("totalEntries: got %d want 1 (%+v)", out.TotalEntries, out.Series)
	}
	surviving, err := trashfile.List(deps.LibRoot, orphan)
	if err != nil {
		t.Fatalf("trashfile.List(%s): %v", orphan, err)
	}
	if len(surviving) != 1 {
		t.Errorf("unindexed trash: got %d buckets want 1 (untouched)", len(surviving))
	}
}
