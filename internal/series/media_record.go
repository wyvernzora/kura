package series

import (
	"context"
)

type mediaRecordInput struct {
	MediaPath      string
	RecordPath     string
	Source         string
	CompanionPaths []mediaRecordCompanionInput
}

type mediaRecordCompanionInput struct {
	MediaPath  string
	RecordPath string
}

func (h Handle) mediaRecord(ctx context.Context, in mediaRecordInput) (MediaRecord, error) {
	info, err := h.inspector().Inspect(ctx, in.MediaPath)
	if err != nil {
		return MediaRecord{}, err
	}
	facts, err := h.files().stat(in.MediaPath)
	if err != nil {
		return MediaRecord{}, err
	}
	source := in.Source
	if source == "" {
		source = ParseMediaSource(inferSourceFromFilename(in.RecordPath)).String()
	}
	record := MediaRecord{
		Path:       in.RecordPath,
		Source:     ParseMediaSource(source).String(),
		Resolution: info.Resolution,
		Codec:      info.VideoCodec,
		Size:       facts.Size,
		MTime:      facts.MTime,
		Companions: []CompanionRecord{},
	}
	for _, companion := range in.CompanionPaths {
		facts, err := h.files().stat(companion.MediaPath)
		if err != nil {
			return MediaRecord{}, err
		}
		record.Companions = append(record.Companions, CompanionRecord{
			Path:  companion.RecordPath,
			Size:  facts.Size,
			MTime: facts.MTime,
		})
	}
	return record, nil
}
