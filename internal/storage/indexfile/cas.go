package indexfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/renameio/v2"
	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/paths"
)

// Entry pairs a metadata ref with the series ref that tracks it. The
// loaded form of index.tsv is a flat slice of Entry, sorted by metadata
// ref string.
type Entry struct {
	Metadata refs.Metadata
	Series   refs.Series
}

// Loaded carries the parsed entries plus the file hash and last-mutated
// stamp parsed from the optional header line. Hash is the SHA-256 of the
// raw on-disk bytes (header + rows) and is what SaveCAS compares against.
type Loaded struct {
	Entries     []Entry
	Hash        string
	LastMutated *coord.Mutator
}

// LoadCAS reads index.tsv and returns the parsed entries, file hash, and
// last-mutated diagnostics. Returns os.ErrNotExist if the file is absent
// (callers translate to whatever scope-specific error fits).
func LoadCAS(root string) (Loaded, error) {
	path := paths.IndexFile(root)
	bytes, err := os.ReadFile(path)
	if err != nil {
		return Loaded{}, err
	}
	return ParseCAS(bytes)
}

// ParseCAS builds a Loaded from already-read bytes. Used by the eventual
// MCP cache watcher which reads the file once per probe and reuses the
// bytes for both the hash compare and (on change) parsing.
func ParseCAS(data []byte) (Loaded, error) {
	out := Loaded{Hash: hashHex(data)}

	// Find the data portion: skip leading comment lines while
	// extracting last_mutated from a `# kura-index last-mutated …` line
	// if present.
	rest, mutator, err := splitHeader(data)
	if err != nil {
		return Loaded{}, err
	}
	out.LastMutated = mutator

	reader := csv.NewReader(bytes.NewReader(rest))
	reader.Comma = '\t'
	reader.FieldsPerRecord = 2
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Loaded{}, fmt.Errorf("indexfile: parse: %w", err)
		}
		metadataRef, err := refs.ParseMetadata(record[0])
		if err != nil {
			return Loaded{}, fmt.Errorf("indexfile: parse: %w", err)
		}
		seriesRef, err := refs.ParseSeries(record[1])
		if err != nil {
			return Loaded{}, fmt.Errorf("indexfile: parse: %w", err)
		}
		out.Entries = append(out.Entries, Entry{Metadata: metadataRef, Series: seriesRef})
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		return out.Entries[i].Metadata.String() < out.Entries[j].Metadata.String()
	})
	return out, nil
}

// SaveCAS atomically writes entries iff the on-disk file still hashes to
// expected. Stamps mutator into the header. expected == "" means "expect
// file does not exist; create fresh".
//
// Returns *coord.ConflictError if drift detected.
func SaveCAS(root string, expected string, entries []Entry, mutator coord.Mutator) error {
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
			if parsed.LastMutated != nil {
				prior = *parsed.LastMutated
			}
			return &coord.ConflictError{Scope: scope, Phase: "pre_write", Mutator: prior}
		}
	} else if _, err := os.Stat(path); err == nil {
		// Caller asked for create-only but file already exists.
		return &coord.ConflictError{Scope: scope, Phase: "pre_write"}
	}

	data, err := encodeEntries(entries, mutator)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(paths.LibraryKuraDir(root), 0o755); err != nil {
		return err
	}
	if err := renameio.WriteFile(path, data, 0o644); err != nil {
		return err
	}

	finalBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if hashHex(finalBytes) != hashHex(data) {
		return &coord.ConflictError{Scope: scope, Phase: "post_write"}
	}
	return nil
}

// ReadHashOnly returns the SHA-256 of the on-disk file bytes without
// parsing entries. Used by the MCP cache fast-probe / refresh tier when
// it just needs a change-detection signal.
func ReadHashOnly(root string) (string, error) {
	data, err := os.ReadFile(paths.IndexFile(root))
	if err != nil {
		return "", err
	}
	return hashHex(data), nil
}

func encodeEntries(entries []Entry, mutator coord.Mutator) ([]byte, error) {
	sorted := make([]Entry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Metadata.String() < sorted[j].Metadata.String()
	})

	var buf bytes.Buffer
	if mutator.Op != "" {
		fmt.Fprintf(&buf, "# kura-index last-mutated op=%s pid=%d host=%s at=%s\n",
			mutator.Op, mutator.PID, escapeHostHeader(mutator.Host),
			mutator.At.UTC().Format(time.RFC3339))
	}
	writer := csv.NewWriter(&buf)
	writer.Comma = '\t'
	for _, entry := range sorted {
		if err := writer.Write([]string{entry.Metadata.String(), entry.Series.String()}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// splitHeader strips leading `#` comment lines from data and returns the
// rest plus a parsed Mutator if a `# kura-index last-mutated …` line is
// present. Other comment lines are silently skipped.
func splitHeader(data []byte) ([]byte, *coord.Mutator, error) {
	var mutator *coord.Mutator
	for {
		nl := bytes.IndexByte(data, '\n')
		if len(data) == 0 || data[0] != '#' {
			return data, mutator, nil
		}
		var line []byte
		if nl == -1 {
			line = data
			data = nil
		} else {
			line = data[:nl]
			data = data[nl+1:]
		}
		if m, ok := parseLastMutatedLine(string(line)); ok {
			mutator = &m
		}
	}
}

func parseLastMutatedLine(line string) (coord.Mutator, bool) {
	const prefix = "# kura-index last-mutated"
	line = strings.TrimRight(line, "\r")
	if !strings.HasPrefix(line, prefix) {
		return coord.Mutator{}, false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	fields := splitHeaderFields(rest)
	out := coord.Mutator{}
	for k, v := range fields {
		switch k {
		case "op":
			out.Op = v
		case "pid":
			n, err := strconv.Atoi(v)
			if err != nil {
				return coord.Mutator{}, false
			}
			out.PID = n
		case "host":
			out.Host = unescapeHostHeader(v)
		case "at":
			at, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return coord.Mutator{}, false
			}
			out.At = at.UTC()
		}
	}
	if out.Op == "" || out.Host == "" || out.At.IsZero() {
		return coord.Mutator{}, false
	}
	return out, true
}

// splitHeaderFields parses a space-separated `key=value` sequence. Values
// don't contain spaces in our format (host is escaped).
func splitHeaderFields(s string) map[string]string {
	out := map[string]string{}
	for _, token := range strings.Fields(s) {
		eq := strings.IndexByte(token, '=')
		if eq <= 0 {
			continue
		}
		out[token[:eq]] = token[eq+1:]
	}
	return out
}

// escapeHostHeader / unescapeHostHeader handle the rare case of spaces
// in a hostname by encoding `%20`. Practical hostnames never contain
// spaces, but keeping the round-trip safe avoids surprises.
func escapeHostHeader(host string) string {
	return strings.ReplaceAll(host, " ", "%20")
}

func unescapeHostHeader(host string) string {
	return strings.ReplaceAll(host, "%20", " ")
}

func hashHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
