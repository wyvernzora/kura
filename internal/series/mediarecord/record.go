package mediarecord

import (
	"context"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/series/layout"
)

var mediaFactsPattern = regexp.MustCompile(`\(([^()]*)\)\.[^.]+$`)

type Builder struct {
	files     layout.Files
	inspector media.Inspector
}

func NewBuilder(files layout.Files, inspector media.Inspector) Builder {
	return Builder{files: files, inspector: inspector}
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

func (b Builder) Build(ctx context.Context, in Input) (media.Record, error) {
	info, err := b.inspector.Inspect(ctx, in.MediaPath)
	if err != nil {
		return media.Record{}, err
	}
	facts, err := b.files.Stat(in.MediaPath)
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
		Size:       facts.Size,
		MTime:      facts.MTime,
		Companions: []media.Companion{},
	}
	for _, companion := range in.CompanionPaths {
		facts, err := b.files.Stat(companion.MediaPath)
		if err != nil {
			return media.Record{}, err
		}
		record.Companions = append(record.Companions, media.Companion{
			Path:  companion.RecordPath,
			Size:  facts.Size,
			MTime: facts.MTime,
		})
	}
	return record, nil
}

func InferSourceFromFilename(path string) string {
	name := filepath.ToSlash(path)
	matches := mediaFactsPattern.FindStringSubmatch(name)
	if len(matches) != 2 {
		return "unknown"
	}
	fields := strings.Fields(matches[1])
	if len(fields) == 0 {
		return "unknown"
	}
	return fields[0]
}

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
