package mediainfo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/wyvernzora/kura/internal/domain/media"
)

var mediaFactsPattern = regexp.MustCompile(`\(([^()]*)\)\.[^.]+$`)

// Builder composes a media.Record from a path on disk: runs mediainfo,
// stats size + mtime, builds companion records.
type Builder struct {
	inspector media.Inspector
}

// NewBuilder returns a Builder backed by the supplied inspector.
func NewBuilder(inspector media.Inspector) Builder {
	return Builder{inspector: inspector}
}

type Input struct {
	MediaPath      string
	RecordPath     string
	Source         string
	CompanionPaths []CompanionInput
}

type CompanionInput struct {
	MediaPath  string
	RecordPath string
}

// Build runs mediainfo + stat against the inputs and returns the
// composed media.Record. RecordPath is the path that gets persisted in
// series.json (typically equal to MediaPath at stage time, or a
// canonical path during scan).
func (b Builder) Build(ctx context.Context, in Input) (media.Record, error) {
	info, err := b.inspector.Inspect(ctx, in.MediaPath)
	if err != nil {
		return media.Record{}, err
	}
	size, mtime, err := statFileFacts(in.MediaPath)
	if err != nil {
		return media.Record{}, err
	}
	source := in.Source
	if source == "" {
		source = InferSourceFromFilename(in.RecordPath)
	}
	resolution, err := media.ParseResolution(info.Resolution)
	if err != nil {
		return media.Record{}, err
	}
	record := media.Record{
		Path:       in.RecordPath,
		Source:     media.ParseSource(source),
		Resolution: resolution,
		Codec:      media.ParseCodec(info.VideoCodec),
		Size:       size,
		MTime:      mtime,
		Companions: []media.Companion{},
	}
	for _, companion := range in.CompanionPaths {
		size, mtime, err := statFileFacts(companion.MediaPath)
		if err != nil {
			return media.Record{}, err
		}
		record.Companions = append(record.Companions, media.Companion{
			Path:  companion.RecordPath,
			Size:  size,
			MTime: mtime,
		})
	}
	return record, nil
}

func statFileFacts(path string) (int64, time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, time.Time{}, err
	}
	if info.IsDir() {
		return 0, time.Time{}, fmt.Errorf("mediainfo: %q is a directory", path)
	}
	return info.Size(), info.ModTime().UTC().Truncate(time.Second), nil
}

// InferSourceFromFilename pulls the source token out of a canonical
// filename like "Foo - S01E01 (WebRip 1080p).mkv". Walks the suffix
// tokens and returns the first one media.IsKnown recognizes; falls
// back to "unknown" when the suffix has only a resolution / codec /
// other non-source token (e.g. "(1280x720)" → unknown).
func InferSourceFromFilename(path string) string {
	name := filepath.ToSlash(path)
	matches := mediaFactsPattern.FindStringSubmatch(name)
	if len(matches) != 2 {
		return "unknown"
	}
	for field := range strings.FieldsSeq(matches[1]) {
		if media.IsKnown(field) {
			return field
		}
	}
	return "unknown"
}

// RecognizedVideoFile reports whether path's extension is a video format
// Kura knows how to track.
func RecognizedVideoFile(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	return slices.Contains([]string{
		".mkv",
		".mp4",
		".m4v",
		".avi",
		".mov",
		".webm",
		".ts",
		".m2ts",
		".wmv",
		".ogm",
		".ogv",
	}, extension)
}
