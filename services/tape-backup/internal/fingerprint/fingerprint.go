// Package fingerprint computes the stat-level identity of archived file trees.
// Its .kura-excluding entry points define the shared plan/executor payload
// scope.
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Fingerprint is a SHA-256 digest of canonical file metadata.
type Fingerprint string

var (
	// ErrSymlink reports that a fingerprint walk found a symlink.
	ErrSymlink = errors.New("fingerprint: symlink")
	// ErrNonRegularFile reports that a fingerprint walk found another
	// unsupported non-regular file.
	ErrNonRegularFile = errors.New("fingerprint: non-regular file")
)

// Entry is one file's stat-level identity.
type Entry struct {
	Path    string
	Size    int64
	ModTime time.Time
}

// Of validates, canonicalizes, and fingerprints entries.
func Of(entries []Entry) (Fingerprint, error) {
	return of(entries, false)
}

// OfExcludingKura fingerprints entries outside the top-level .kura directory.
func OfExcludingKura(entries []Entry) (Fingerprint, error) {
	return of(entries, true)
}

func of(entries []Entry, excludeKura bool) (Fingerprint, error) {
	canonical := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		normalized, err := canonicalEntry(entry)
		if err != nil {
			return "", err
		}
		if excludeKura && excludedKuraPath(normalized.Path) {
			continue
		}
		canonical = append(canonical, normalized)
	}
	slices.SortFunc(canonical, func(a, b Entry) int {
		return strings.Compare(a.Path, b.Path)
	})

	hash := sha256.New()
	for i, entry := range canonical {
		if i > 0 && entry.Path == canonical[i-1].Path {
			return "", fmt.Errorf("fingerprint: duplicate path %q", entry.Path)
		}
		if _, err := io.WriteString(
			hash,
			entry.Path+"\x00"+
				strconv.FormatInt(entry.Size, 10)+"\x00"+
				strconv.FormatInt(entry.ModTime.Unix(), 10)+"\n",
		); err != nil {
			return "", fmt.Errorf("fingerprint: hash entry %q: %w", entry.Path, err)
		}
	}
	return Fingerprint("sha256:" + hex.EncodeToString(hash.Sum(nil))), nil
}

// Compute fingerprints every regular file under root. Non-regular files are
// rejected and empty directories do not contribute.
func Compute(root string) (Fingerprint, error) {
	return compute(root, false)
}

// ComputeExcludingKura fingerprints regular files outside root/.kura.
func ComputeExcludingKura(root string) (Fingerprint, error) {
	return compute(root, true)
}

func compute(root string, excludeKura bool) (Fingerprint, error) {
	entries := make([]Entry, 0)
	err := filepath.WalkDir(root, func(filePath string, dirEntry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("fingerprint: walk %q: %w", filePath, walkErr)
		}
		if filePath == root {
			if dirEntry.Type()&os.ModeSymlink != 0 {
				return errors.New("fingerprint: root must not be a symlink")
			}
			if !dirEntry.IsDir() {
				return errors.New("fingerprint: root must be a directory")
			}
			return nil
		}

		relativePath, err := filepath.Rel(root, filePath)
		if err != nil {
			return fmt.Errorf("fingerprint: relative path for %q: %w", filePath, err)
		}
		relativePath = filepath.ToSlash(relativePath)
		if excludeKura && excludedKuraPath(relativePath) {
			if dirEntry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if dirEntry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w %q is not supported", ErrSymlink, relativePath)
		}
		if dirEntry.IsDir() {
			return nil
		}

		info, err := dirEntry.Info()
		if err != nil {
			return fmt.Errorf("fingerprint: stat %q: %w", relativePath, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf(
				"%w %q is not supported",
				ErrNonRegularFile,
				relativePath,
			)
		}
		entries = append(entries, Entry{
			Path:    relativePath,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
		return nil
	})
	if err != nil {
		return "", err
	}
	return of(entries, excludeKura)
}

func excludedKuraPath(relativePath string) bool {
	return relativePath == ".kura" || strings.HasPrefix(relativePath, ".kura/")
}

func canonicalEntry(entry Entry) (Entry, error) {
	if entry.Path == "" {
		return Entry{}, errors.New("fingerprint: path is required")
	}
	if !utf8.ValidString(entry.Path) {
		return Entry{}, fmt.Errorf("fingerprint: path %q is not valid UTF-8", entry.Path)
	}
	if strings.ContainsAny(entry.Path, "\x00\n") {
		return Entry{}, fmt.Errorf(
			"fingerprint: path %q must not contain NUL or newline",
			entry.Path,
		)
	}

	normalizedPath := norm.NFC.String(filepath.ToSlash(entry.Path))
	if path.IsAbs(normalizedPath) {
		return Entry{}, fmt.Errorf("fingerprint: path %q must be relative", entry.Path)
	}
	for part := range strings.SplitSeq(normalizedPath, "/") {
		if part == ".." {
			return Entry{}, fmt.Errorf("fingerprint: path %q must not contain ..", entry.Path)
		}
	}
	if path.Clean(normalizedPath) != normalizedPath || normalizedPath == "." {
		return Entry{}, fmt.Errorf("fingerprint: path %q is not canonical", entry.Path)
	}
	if entry.Size < 0 {
		return Entry{}, fmt.Errorf("fingerprint: size for %q must not be negative", normalizedPath)
	}
	if entry.ModTime.IsZero() {
		return Entry{}, fmt.Errorf("fingerprint: mtime for %q is required", normalizedPath)
	}

	entry.Path = normalizedPath
	return entry, nil
}
