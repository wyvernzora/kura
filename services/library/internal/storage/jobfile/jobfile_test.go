package jobfile_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/wyvernzora/kura/services/library/internal/storage/jobfile"
	"github.com/wyvernzora/kura/services/library/internal/storage/paths"
)

func TestRoundTripHeaderProgressTerminal(t *testing.T) {
	root := t.TempDir()
	w, err := jobfile.Create(root, jobfile.HeaderLine{
		JobID:     "01J0",
		Kind:      "scan",
		SeriesRef: "Anime/Frieren",
		StartedAt: "2026-05-09T17:30:00Z",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := w.AppendProgress(jobfile.ProgressLine{
		At: "2026-05-09T17:30:01Z", Status: "update", Current: 1, Total: 3, Message: "ep1",
	}); err != nil {
		t.Fatalf("AppendProgress: %v", err)
	}
	if err := w.AppendProgress(jobfile.ProgressLine{
		At: "2026-05-09T17:30:02Z", Status: "update", Current: 2, Total: 3, Message: "ep2",
	}); err != nil {
		t.Fatalf("AppendProgress: %v", err)
	}
	if err := w.AppendTerminal(jobfile.TerminalLine{
		At:     "2026-05-09T17:30:03Z",
		State:  "succeeded",
		Result: json.RawMessage(`{"synced":3}`),
	}); err != nil {
		t.Fatalf("AppendTerminal: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := jobfile.Read(root, "01J0")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Header.JobID != "01J0" || got.Header.Kind != "scan" || got.Header.SeriesRef != "Anime/Frieren" {
		t.Fatalf("header roundtrip: %+v", got.Header)
	}
	if len(got.Progress) != 2 {
		t.Fatalf("progress count = %d, want 2", len(got.Progress))
	}
	if got.Progress[1].Current != 2 {
		t.Fatalf("progress[1].Current = %d, want 2", got.Progress[1].Current)
	}
	if got.Terminal == nil || got.Terminal.State != "succeeded" {
		t.Fatalf("terminal: %+v", got.Terminal)
	}
	if string(got.Terminal.Result) != `{"synced":3}` {
		t.Fatalf("terminal.Result = %s", got.Terminal.Result)
	}
}

func TestReadMissingTerminalReturnsNil(t *testing.T) {
	root := t.TempDir()
	w, err := jobfile.Create(root, jobfile.HeaderLine{
		JobID: "01J1", Kind: "scan", StartedAt: "2026-05-09T17:30:00Z",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := w.AppendProgress(jobfile.ProgressLine{Current: 1, Total: 3}); err != nil {
		t.Fatalf("AppendProgress: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := jobfile.Read(root, "01J1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Terminal != nil {
		t.Fatalf("Terminal = %+v, want nil", got.Terminal)
	}
	if len(got.Progress) != 1 {
		t.Fatalf("progress count = %d, want 1", len(got.Progress))
	}
}

func TestReadTolerantOfTruncatedLastLine(t *testing.T) {
	root := t.TempDir()
	w, err := jobfile.Create(root, jobfile.HeaderLine{
		JobID: "01J2", Kind: "scan", StartedAt: "2026-05-09T17:30:00Z",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := w.AppendProgress(jobfile.ProgressLine{Current: 1, Total: 3}); err != nil {
		t.Fatalf("AppendProgress: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Append a corrupt line by hand.
	path := paths.JobFile(root, "01J2")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString(`{"type":"progress","at":"...","current":2,"to`); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	got, err := jobfile.Read(root, "01J2")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got.Progress) != 1 {
		t.Fatalf("progress count = %d, want 1 (truncated line ignored)", len(got.Progress))
	}
}

func TestReadMissingFileReturnsNotExist(t *testing.T) {
	root := t.TempDir()
	_, err := jobfile.Read(root, "01JNOPE")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestCreateRefusesExistingFile(t *testing.T) {
	root := t.TempDir()
	header := jobfile.HeaderLine{JobID: "01J3", Kind: "scan", StartedAt: "2026-05-09T17:30:00Z"}
	w, err := jobfile.Create(root, header)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := jobfile.Create(root, header); err == nil {
		t.Fatalf("second Create succeeded; want O_EXCL collision")
	}
}

func TestCreateAutoCreatesJobsDir(t *testing.T) {
	root := t.TempDir()
	if _, err := os.Stat(filepath.Join(root, ".kura", "jobs")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("jobs dir already exists: %v", err)
	}
	w, err := jobfile.Create(root, jobfile.HeaderLine{
		JobID: "01J4", Kind: "scan", StartedAt: "2026-05-09T17:30:00Z",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".kura", "jobs")); err != nil {
		t.Fatalf("jobs dir missing post-Create: %v", err)
	}
}
