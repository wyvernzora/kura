package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/series"
)

func TestPlanExtras_EmptyArrayProducesNoSteps(t *testing.T) {
	model := &series.Series{}
	steps, err := planExtras(testToken, model, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 0 {
		t.Fatalf("steps = %d, want 0", len(steps))
	}
}

func TestPlanExtras_FileSourceEmitsOneFileMove(t *testing.T) {
	inbox := t.TempDir()
	if err := os.WriteFile(filepath.Join(inbox, "bts.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := ulid.Make()
	model := &series.Series{
		StagedExtras: []series.StagedExtraItem{{
			ID: id, Season: 1, Path: "inbox:bts.mp4",
		}},
	}
	steps, err := planExtras(testToken, model, inbox)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	if steps[0].Kind != StepFileMove {
		t.Errorf("kind = %q", steps[0].Kind)
	}
	if steps[0].Owner.Season != 1 {
		t.Errorf("Season = %d", steps[0].Owner.Season)
	}
}

func TestPlanExtras_DirectorySourceEmitsFilesThenDirRemovesDeepestFirst(t *testing.T) {
	inbox := t.TempDir()
	root := filepath.Join(inbox, "extras")
	deep := filepath.Join(root, "sub", "deeper")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "top.mp4"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "mid.mp4"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "leaf.mp4"), []byte("c"), 0o644); err != nil {
		t.Fatal(err)
	}

	id := ulid.Make()
	model := &series.Series{
		StagedExtras: []series.StagedExtraItem{{
			ID: id, Season: 1, Path: "inbox:extras",
		}},
	}
	steps, err := planExtras(testToken, model, inbox)
	if err != nil {
		t.Fatal(err)
	}
	// Expected: 3 file_move + 2 dir_remove (sub/deeper, sub) + 1
	// dir_remove (root) = 6 total.
	if len(steps) != 6 {
		t.Fatalf("steps = %d, want 6\n%+v", len(steps), steps)
	}

	// First three steps are file_moves; remaining are dir_removes.
	for i := range 3 {
		if steps[i].Kind != StepFileMove {
			t.Errorf("step[%d] kind = %q, want file_move", i, steps[i].Kind)
		}
	}
	for i := 3; i < 6; i++ {
		if steps[i].Kind != StepDirRemove {
			t.Errorf("step[%d] kind = %q, want dir_remove", i, steps[i].Kind)
		}
	}

	// File moves: deepest first. leaf.mp4 (depth 3 in source tree) before mid.mp4 (depth 2) before top.mp4 (depth 1).
	wantFiles := []string{
		filepath.Join(inbox, "extras", "sub", "deeper", "leaf.mp4"),
		filepath.Join(inbox, "extras", "sub", "mid.mp4"),
		filepath.Join(inbox, "extras", "top.mp4"),
	}
	for i, want := range wantFiles {
		if steps[i].From != want {
			t.Errorf("file[%d] From = %q, want %q", i, steps[i].From, want)
		}
	}

	// dir_removes: deeper before sub before root (selector form for top-level).
	wantDirs := []string{
		filepath.Join(inbox, "extras", "sub", "deeper"),
		filepath.Join(inbox, "extras", "sub"),
		"inbox:extras",
	}
	for i, want := range wantDirs {
		if steps[3+i].Path != want {
			t.Errorf("dir_remove[%d] Path = %q, want %q", i, steps[3+i].Path, want)
		}
	}
}

func TestPlanExtras_MissingSourceErrors(t *testing.T) {
	inbox := t.TempDir()
	id := ulid.Make()
	model := &series.Series{
		StagedExtras: []series.StagedExtraItem{{
			ID: id, Season: 1, Path: "inbox:nonexistent",
		}},
	}
	if _, err := planExtras(testToken, model, inbox); err == nil {
		t.Fatal("planExtras: nil err for missing source")
	}
}

func TestPlanExtras_AbsolutePathRejected(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "bts.mp4")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := ulid.Make()
	model := &series.Series{
		StagedExtras: []series.StagedExtraItem{{
			ID: id, Season: 1, Path: src,
		}},
	}
	_, err := planExtras(testToken, model, dir)
	if err == nil {
		t.Fatal("planExtras: nil err for absolute path (expected rejection)")
	}
}
