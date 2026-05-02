package state

import (
	"fmt"
	"sort"
	"time"

	"github.com/wyvernzora/kura/internal/domain/media"
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
		active, err := fromWireMedia(episode.Active)
		if err != nil {
			return State{}, err
		}
		staged, err := fromWireMedia(episode.Staged)
		if err != nil {
			return State{}, err
		}
		airDate, err := parseAirDate(episode.AirDate)
		if err != nil {
			return State{}, fmt.Errorf("series: invalid air date %q: %w", episode.AirDate, err)
		}
		out.Episodes[ref] = Episode{
			AirDate: airDate,
			Active:  active,
			Staged:  staged,
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
		var airDate string
		if episode.AirDate.IsValid() {
			airDate = episode.AirDate.String()
		}
		out.Episodes[ref.String()] = wire.EpisodeV1{
			AirDate: airDate,
			Active:  toWireMedia(episode.Active),
			Staged:  toWireMedia(episode.Staged),
		}
	}
	return out, nil
}

func fromWireMedia(in *wire.MediaRecordV1) (*media.Record, error) {
	if in == nil {
		return nil, nil
	}
	resolution, err := media.ParseResolution(in.Resolution)
	if err != nil {
		return nil, fmt.Errorf("series: invalid resolution %q: %w", in.Resolution, err)
	}
	out := media.Record{
		Path:       in.Path,
		Source:     media.ParseSource(in.Source),
		Resolution: resolution,
		Codec:      media.ParseCodec(in.Codec),
		Size:       in.Size,
		Companions: make([]media.Companion, 0, len(in.Companions)),
	}
	if in.MTime != "" {
		if parsed, err := time.Parse(time.RFC3339, in.MTime); err == nil {
			out.MTime = parsed
		}
	}
	for _, companion := range in.Companions {
		out.Companions = append(out.Companions, fromWireCompanion(companion))
	}
	return &out, nil
}

func toWireMedia(in *media.Record) *wire.MediaRecordV1 {
	if in == nil {
		return nil
	}
	out := wire.MediaRecordV1{
		Path:       in.Path,
		Source:     in.Source.String(),
		Resolution: in.Resolution.String(),
		Codec:      in.Codec.String(),
		Size:       in.Size,
		MTime:      in.MTime.UTC().Format(time.RFC3339),
		Companions: make([]wire.CompanionRecordV1, 0, len(in.Companions)),
	}
	for _, companion := range in.Companions {
		out.Companions = append(out.Companions, toWireCompanion(companion))
	}
	return &out
}

func fromWireCompanion(in wire.CompanionRecordV1) media.Companion {
	out := media.Companion{
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

func toWireCompanion(in media.Companion) wire.CompanionRecordV1 {
	return wire.CompanionRecordV1{
		Path:     in.Path,
		Role:     in.Role,
		Language: in.Language,
		Label:    in.Label,
		Size:     in.Size,
		MTime:    in.MTime.UTC().Format(time.RFC3339),
	}
}
