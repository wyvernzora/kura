package jobfile

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/wyvernzora/kura/internal/storage/paths"
)

// Writer is an open append handle to a job's JSONL file. Created
// once per job; the registry's per-job goroutine is the only writer
// during the job's lifetime.
//
// Every Append* call ends with f.Sync() so a crash loses at most one
// event. Mirrors the recipe in internal/storage/planfile.Log.
type Writer struct {
	mu      sync.Mutex
	file    *os.File
	encoder *json.Encoder
}

// Create opens <libRoot>/.kura/jobs/<header.JobID>.jsonl with
// O_CREATE|O_EXCL|O_WRONLY, writes the header, fsyncs, and returns a
// Writer pinned for append. Errors propagate to the caller; the
// registry treats persistence as opportunistic (logs and continues).
//
// Auto-creates the parent directory the first time a job is
// submitted in a fresh library — paths.JobsDir(libRoot) doesn't
// exist by default.
func Create(libRoot string, header HeaderLine) (*Writer, error) {
	if header.JobID == "" {
		return nil, fmt.Errorf("jobfile: empty JobID")
	}
	if err := os.MkdirAll(paths.JobsDir(libRoot), 0o775); err != nil {
		return nil, fmt.Errorf("jobfile: mkdir jobs dir: %w", err)
	}
	header.Type = LineHeader
	if header.SchemaVersion == 0 {
		header.SchemaVersion = SchemaVersion
	}
	path := paths.JobFile(libRoot, header.JobID)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o664)
	if err != nil {
		return nil, fmt.Errorf("jobfile: open %s: %w", path, err)
	}
	w := &Writer{file: file, encoder: json.NewEncoder(file)}
	if err := w.encoder.Encode(header); err != nil {
		_ = w.file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("jobfile: write header: %w", err)
	}
	if err := w.file.Sync(); err != nil {
		_ = w.file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("jobfile: sync header: %w", err)
	}
	return w, nil
}

// AppendProgress writes one progress line and fsyncs. Safe to call
// concurrently across goroutines, though in practice the registry
// owns a single writer per job.
func (w *Writer) AppendProgress(line ProgressLine) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	line.Type = LineProgress
	if err := w.encoder.Encode(line); err != nil {
		return err
	}
	return w.file.Sync()
}

// AppendTerminal writes the closing line and fsyncs. Caller is
// expected to call Close after this.
func (w *Writer) AppendTerminal(line TerminalLine) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	line.Type = LineTerminal
	if err := w.encoder.Encode(line); err != nil {
		return err
	}
	return w.file.Sync()
}

// Close releases the file handle. Idempotent; safe to defer.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}
