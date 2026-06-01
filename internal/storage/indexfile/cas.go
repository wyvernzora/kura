package indexfile

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"time"

	"github.com/google/renameio/v2/maybe"
	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/storage/paths"
)

// ErrSchemaMismatch is returned by LoadCAS when the on-disk header carries
// a SchemaVersion the build can't read. Callers force a rebuild and rewrite
// at the current SchemaVersion.
var ErrSchemaMismatch = errors.New("indexfile: schema version mismatch")

// Loaded carries the parsed header, rows, and the file hash used by SaveCAS
// for optimistic-concurrency comparison. Hash is the SHA-256 of the raw
// on-disk bytes (header line + row lines + trailing newline).
type Loaded struct {
	Header Header
	Rows   []Row
	Hash   string
}

// LoadCAS reads index.jsonl, parses header + rows, and returns the bytes
// hash. Returns os.ErrNotExist when the file is absent.
func LoadCAS(root string) (Loaded, error) {
	data, err := os.ReadFile(paths.IndexFile(root))
	if err != nil {
		return Loaded{}, err
	}
	return ParseCAS(data)
}

// ParseCAS builds a Loaded from already-read bytes. The watcher uses this
// to share one read between hash compare and (on change) row parse.
//
// Empty files are rejected; index.jsonl always carries at least a header.
func ParseCAS(data []byte) (Loaded, error) {
	out := Loaded{Hash: hashHex(data)}
	if len(bytes.TrimSpace(data)) == 0 {
		return Loaded{}, fmt.Errorf("indexfile: parse: empty file")
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20) // 1 MiB max line; longest realistic row is far less

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return Loaded{}, fmt.Errorf("indexfile: parse: %w", err)
		}
		return Loaded{}, fmt.Errorf("indexfile: parse: missing header")
	}
	if err := json.Unmarshal(scanner.Bytes(), &out.Header); err != nil {
		return Loaded{}, fmt.Errorf("indexfile: parse header: %w", err)
	}
	if out.Header.SchemaVersion != SchemaVersion {
		return Loaded{}, fmt.Errorf("%w: got %d, want %d", ErrSchemaMismatch, out.Header.SchemaVersion, SchemaVersion)
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var row Row
		if err := json.Unmarshal(line, &row); err != nil {
			return Loaded{}, fmt.Errorf("indexfile: parse row: %w", err)
		}
		out.Rows = append(out.Rows, row)
	}
	if err := scanner.Err(); err != nil {
		return Loaded{}, fmt.Errorf("indexfile: parse: %w", err)
	}

	sort.Slice(out.Rows, func(i, j int) bool {
		return out.Rows[i].Series.String() < out.Rows[j].Series.String()
	})
	return out, nil
}

// SaveCAS atomically writes rows iff the on-disk file still hashes to
// expected. Stamps mutator into the header. expected == "" means
// "expect file does not exist; create fresh".
//
// Rows are sorted by Series ref before writing so two rebuilds with the
// same input produce byte-identical files.
//
// Returns *coord.ConflictError on drift.
func SaveCAS(root string, expected string, rows []Row, mutator coord.Mutator) error {
	path := paths.IndexFile(root)
	scope := coord.LibraryScope

	if expected != "" {
		current, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return &coord.ConflictError{Scope: scope, Phase: "pre_write"}
			}
			return err
		}
		if hashHex(current) != expected {
			parsed, _ := ParseCAS(current)
			var prior coord.Mutator
			if parsed.Header.LastMutated != nil {
				prior = *parsed.Header.LastMutated
			}
			return &coord.ConflictError{Scope: scope, Phase: "pre_write", Mutator: prior}
		}
	} else if _, err := os.Stat(path); err == nil {
		return &coord.ConflictError{Scope: scope, Phase: "pre_write"}
	}

	header := Header{
		SchemaVersion: SchemaVersion,
		IndexAsOf:     mutator.At.UTC().Format(time.RFC3339),
	}
	if mutator.Op != "" {
		stamp := mutator
		stamp.At = stamp.At.UTC()
		header.LastMutated = &stamp
	}
	data, err := encode(header, rows)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(paths.LibraryKuraDir(root), 0o775); err != nil {
		return err
	}
	if err := maybe.WriteFile(path, data, 0o664); err != nil {
		return err
	}

	finalBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if hashHex(finalBytes) != hashHex(data) {
		return &coord.ConflictError{Scope: scope, Phase: "post_write"}
	}
	slog.Debug("indexfile write",
		"path", path,
		"rows", len(rows),
		"op", mutator.Op,
	)
	return nil
}

// ReadHashOnly returns the SHA-256 of the on-disk file bytes without
// parsing. Used by the watcher's fast probe to detect peer mutations.
func ReadHashOnly(root string) (string, error) {
	data, err := os.ReadFile(paths.IndexFile(root))
	if err != nil {
		return "", err
	}
	return hashHex(data), nil
}

func encode(header Header, rows []Row) ([]byte, error) {
	sorted := make([]Row, len(rows))
	copy(sorted, rows)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Series.String() < sorted[j].Series.String()
	})

	var buf bytes.Buffer
	if err := writeJSONLine(&buf, header); err != nil {
		return nil, err
	}
	for i := range sorted {
		if err := writeJSONLine(&buf, sorted[i]); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func writeJSONLine(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	_, err = w.Write([]byte{'\n'})
	return err
}

func hashHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
