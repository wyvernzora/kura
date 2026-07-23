package indexfile

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/google/renameio/v2/maybe"
	"github.com/wyvernzora/kura/services/library/internal/coord"
	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library/internal/storage/paths"
	"github.com/wyvernzora/kura/services/library/internal/storage/seriesfile"
)

type snapshotLine struct {
	Series refs.Series     `json:"series"`
	Model  json.RawMessage `json:"model,omitempty"`
	Error  string          `json:"error,omitempty"`
}

func readSnapshot(root string) (map[refs.Series]entry, error) {
	data, err := os.ReadFile(paths.IndexFile(root))
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("indexfile: parse: empty file")
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 8<<20)
	if err := readHeader(scanner); err != nil {
		return nil, err
	}

	out := map[refs.Series]entry{}
	byMeta := map[refs.Metadata]refs.Series{}
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		e, err := parseEntry(root, scanner.Bytes(), byMeta)
		if err != nil {
			return nil, err
		}
		out[e.series] = e
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("indexfile: parse: %w", err)
	}
	return out, nil
}

func readHeader(scanner *bufio.Scanner) error {
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("indexfile: parse: %w", err)
		}
		return fmt.Errorf("indexfile: parse: missing header")
	}
	var header Header
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		return fmt.Errorf("indexfile: parse header: %w", err)
	}
	if header.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: got %d, want %d", ErrSchemaMismatch, header.SchemaVersion, SchemaVersion)
	}
	return nil
}

func parseEntry(root string, line []byte, byMeta map[refs.Metadata]refs.Series) (entry, error) {
	var wire snapshotLine
	if err := json.Unmarshal(line, &wire); err != nil {
		return entry{}, fmt.Errorf("indexfile: parse entry: %w", err)
	}
	if wire.Series.IsZero() {
		return entry{}, fmt.Errorf("indexfile: parse entry: series is required")
	}
	e := entry{series: wire.Series, err: wire.Error}
	if len(wire.Model) == 0 {
		return e, nil
	}
	model, err := seriesfile.Decode(root, wire.Series, wire.Model)
	if err != nil {
		return entry{}, fmt.Errorf("indexfile: parse model %s: %w", wire.Series, err)
	}
	if err := recordMetadataRef(byMeta, model.Metadata, wire.Series); err != nil {
		return entry{}, err
	}
	e.model = model
	e.raw = append(json.RawMessage(nil), wire.Model...)
	return e, nil
}

func recordMetadataRef(byMeta map[refs.Metadata]refs.Series, metadata refs.Metadata, series refs.Series) error {
	if metadata == "" {
		return nil
	}
	if existing, ok := byMeta[metadata]; ok && existing != series {
		return DuplicateRefError{Ref: metadata, Existing: existing, Next: series}
	}
	byMeta[metadata] = series
	return nil
}

func writeSnapshot(root string, entries []entry, mutator coord.Mutator) error {
	now := mutator.At
	if now.IsZero() {
		now = time.Now()
	}
	header := Header{
		SchemaVersion: SchemaVersion,
		IndexAsOf:     now.UTC().Format(time.RFC3339),
	}
	if mutator.Op != "" {
		stamp := mutator
		if stamp.At.IsZero() {
			stamp.At = now
		}
		stamp.At = stamp.At.UTC()
		header.LastMutated = &stamp
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].series.String() < entries[j].series.String()
	})
	var buf bytes.Buffer
	if err := writeJSONLine(&buf, header); err != nil {
		return err
	}
	for _, e := range entries {
		line := snapshotLine{Series: e.series, Error: e.err}
		if e.model != nil {
			line.Model = e.raw
		}
		if err := writeJSONLine(&buf, line); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(paths.LibraryKuraDir(root), 0o775); err != nil {
		return err
	}
	return maybe.WriteFile(paths.IndexFile(root), buf.Bytes(), 0o664)
}

func writeJSONLine(buf *bytes.Buffer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := buf.Write(data); err != nil {
		return err
	}
	return buf.WriteByte('\n')
}
