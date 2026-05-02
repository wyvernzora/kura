package state

import (
	"fmt"
	"sort"
	"time"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/series/wire"
	"github.com/wyvernzora/kura/internal/textnorm"
)

func FromWire(in wire.SeriesV1) (State, error) {
	metadata, err := refs.ParseMetadata(in.MetadataRef)
	if err != nil {
		return State{}, err
	}
	out := State{
		Metadata:       metadata,
		PreferredTitle: textnorm.NFC(in.PreferredTitle),
		CanonicalTitle: textnorm.NFC(in.CanonicalTitle),
		Episodes:       map[refs.Episode]Episode{},
	}
	if in.LastScanned != "" {
		lastScanned, err := time.Parse(time.RFC3339, in.LastScanned)
		if err != nil {
			return State{}, fmt.Errorf("series: invalid lastScanned %q: %w", in.LastScanned, err)
		}
		out.LastScanned = lastScanned
	}
	for key, episode := range in.Episodes {
		ref, err := refs.ParseEpisode(key)
		if err != nil {
			return State{}, err
		}
		out.Episodes[ref] = Episode{
			AirDate: episode.AirDate,
			Active:  fromWireMedia(episode.Active),
			Staged:  fromWireMedia(episode.Staged),
		}
	}
	return out, nil
}

func ToWire(in State) (wire.SeriesV1, error) {
	out := wire.SeriesV1{
		SchemaVersion:  wire.CurrentSchemaVersion,
		MetadataRef:    in.Metadata.String(),
		PreferredTitle: in.PreferredTitle.String(),
		CanonicalTitle: in.CanonicalTitle.String(),
		Episodes:       map[string]wire.EpisodeV1{},
	}
	if !in.LastScanned.IsZero() {
		out.LastScanned = in.LastScanned.UTC().Format(time.RFC3339)
	}
	keys := make([]refs.Episode, 0, len(in.Episodes))
	for ref := range in.Episodes {
		keys = append(keys, ref)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	for _, ref := range keys {
		episode := in.Episodes[ref]
		out.Episodes[ref.String()] = wire.EpisodeV1{
			AirDate: episode.AirDate,
			Active:  toWireMedia(episode.Active),
			Staged:  toWireMedia(episode.Staged),
		}
	}
	return out, nil
}

func fromWireMedia(in *wire.MediaRecordV1) *MediaRecord {
	if in == nil {
		return nil
	}
	out := MediaRecord{
		Path:       in.Path,
		Source:     in.Source,
		Resolution: in.Resolution,
		Codec:      in.Codec,
		Size:       in.Size,
		Companions: make([]CompanionRecord, 0, len(in.Companions)),
	}
	if in.MTime != "" {
		if parsed, err := time.Parse(time.RFC3339, in.MTime); err == nil {
			out.MTime = parsed
		}
	}
	for _, companion := range in.Companions {
		out.Companions = append(out.Companions, fromWireCompanion(companion))
	}
	return &out
}

func toWireMedia(in *MediaRecord) *wire.MediaRecordV1 {
	if in == nil {
		return nil
	}
	out := wire.MediaRecordV1{
		Path:       in.Path,
		Source:     in.Source,
		Resolution: in.Resolution,
		Codec:      in.Codec,
		Size:       in.Size,
		MTime:      in.MTime.UTC().Format(time.RFC3339),
		Companions: make([]wire.CompanionRecordV1, 0, len(in.Companions)),
	}
	for _, companion := range in.Companions {
		out.Companions = append(out.Companions, toWireCompanion(companion))
	}
	return &out
}

func fromWireCompanion(in wire.CompanionRecordV1) CompanionRecord {
	out := CompanionRecord{
		Path:     in.Path,
		Role:     in.Role,
		Language: in.Language,
		Label:    in.Label,
		Size:     in.Size,
	}
	if in.MTime != "" {
		if parsed, err := time.Parse(time.RFC3339, in.MTime); err == nil {
			out.MTime = parsed
		}
	}
	return out
}

func toWireCompanion(in CompanionRecord) wire.CompanionRecordV1 {
	return wire.CompanionRecordV1{
		Path:     in.Path,
		Role:     in.Role,
		Language: in.Language,
		Label:    in.Label,
		Size:     in.Size,
		MTime:    in.MTime.UTC().Format(time.RFC3339),
	}
}
