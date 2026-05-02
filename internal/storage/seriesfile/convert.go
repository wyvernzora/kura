package seriesfile

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/textnorm"
)

func fromWire(in seriesV1) (*series.Series, error) {
	metadataRef, err := refs.ParseMetadata(in.MetadataRef)
	if err != nil {
		return nil, err
	}
	out := &series.Series{
		Metadata:       metadataRef,
		PreferredTitle: textnorm.NFC(in.PreferredTitle),
		CanonicalTitle: textnorm.NFC(in.CanonicalTitle),
		Episodes:       map[refs.Episode]series.Episode{},
	}
	if in.LastScanned != "" {
		lastScanned, err := time.Parse(time.RFC3339, in.LastScanned)
		if err != nil {
			return nil, fmt.Errorf("seriesfile: invalid lastScanned %q: %w", in.LastScanned, err)
		}
		out.LastScanned = lastScanned
	}
	for key, ep := range in.Episodes {
		ref, err := refs.ParseEpisode(key)
		if err != nil {
			return nil, err
		}
		active, err := mediaFromWire(ep.Active)
		if err != nil {
			return nil, err
		}
		staged, err := mediaFromWire(ep.Staged)
		if err != nil {
			return nil, err
		}
		air, err := series.ParseAirDate(ep.AirDate)
		if err != nil {
			return nil, fmt.Errorf("seriesfile: invalid air date %q: %w", ep.AirDate, err)
		}
		out.Episodes[ref] = series.Episode{AirDate: air, Active: active, Staged: staged}
	}
	return out, nil
}

func toWire(in *series.Series) seriesV1 {
	out := seriesV1{
		SchemaVersion:  currentSchemaVersion,
		MetadataRef:    in.Metadata.String(),
		PreferredTitle: in.PreferredTitle.String(),
		CanonicalTitle: in.CanonicalTitle.String(),
		Episodes:       map[string]episodeV1{},
	}
	if !in.LastScanned.IsZero() {
		out.LastScanned = in.LastScanned.UTC().Format(time.RFC3339)
	}
	keys := make([]refs.Episode, 0, len(in.Episodes))
	for r := range in.Episodes {
		keys = append(keys, r)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	for _, ref := range keys {
		ep := in.Episodes[ref]
		var air string
		if ep.AirDate.IsValid() {
			air = ep.AirDate.String()
		}
		out.Episodes[ref.String()] = episodeV1{
			AirDate: air,
			Active:  mediaToWire(ep.Active),
			Staged:  mediaToWire(ep.Staged),
		}
	}
	return out
}

func mediaFromWire(in *mediaRecordV1) (*media.Record, error) {
	if in == nil {
		return nil, nil
	}
	resolution, err := media.ParseResolution(in.Resolution)
	if err != nil {
		return nil, fmt.Errorf("seriesfile: invalid resolution %q: %w", in.Resolution, err)
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
		out.Companions = append(out.Companions, companionFromWire(companion))
	}
	return &out, nil
}

func mediaToWire(in *media.Record) *mediaRecordV1 {
	if in == nil {
		return nil
	}
	out := mediaRecordV1{
		Path:       in.Path,
		Source:     in.Source.String(),
		Resolution: in.Resolution.String(),
		Codec:      in.Codec.String(),
		Size:       in.Size,
		MTime:      in.MTime.UTC().Format(time.RFC3339),
		Companions: make([]companionRecordV1, 0, len(in.Companions)),
	}
	for _, companion := range in.Companions {
		out.Companions = append(out.Companions, companionToWire(companion))
	}
	return &out
}

func companionFromWire(in companionRecordV1) media.Companion {
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

func companionToWire(in media.Companion) companionRecordV1 {
	return companionRecordV1{
		Path:     in.Path,
		Role:     in.Role,
		Language: in.Language,
		Label:    in.Label,
		Size:     in.Size,
		MTime:    in.MTime.UTC().Format(time.RFC3339),
	}
}

// absolutizeActive joins relative active record paths with seriesDir. Staged
// paths live outside the series dir and are already absolute on disk.
func absolutizeActive(s *series.Series, seriesDir string) {
	for ref, ep := range s.Episodes {
		if ep.Active != nil {
			absolutizeRecord(ep.Active, seriesDir)
			s.Episodes[ref] = ep
		}
	}
}

func absolutizeRecord(r *media.Record, seriesDir string) {
	if !filepath.IsAbs(r.Path) {
		r.Path = filepath.Join(seriesDir, filepath.FromSlash(r.Path))
	}
	for i, c := range r.Companions {
		if !filepath.IsAbs(c.Path) {
			r.Companions[i].Path = filepath.Join(seriesDir, filepath.FromSlash(c.Path))
		}
	}
}

// relativizeActiveWire rewrites active record paths inside w from absolute
// (memory shape) to series-dir-relative slashed paths (wire shape). Staged
// paths stay absolute.
func relativizeActiveWire(w *seriesV1, seriesDir string) error {
	for key, ep := range w.Episodes {
		if ep.Active != nil {
			if err := relativizeWireRecord(ep.Active, seriesDir); err != nil {
				return err
			}
			w.Episodes[key] = ep
		}
	}
	return nil
}

func relativizeWireRecord(r *mediaRecordV1, seriesDir string) error {
	if filepath.IsAbs(r.Path) {
		rel, err := filepath.Rel(seriesDir, r.Path)
		if err != nil {
			return err
		}
		r.Path = filepath.ToSlash(rel)
	}
	for i, c := range r.Companions {
		if filepath.IsAbs(c.Path) {
			rel, err := filepath.Rel(seriesDir, c.Path)
			if err != nil {
				return err
			}
			r.Companions[i].Path = filepath.ToSlash(rel)
		}
	}
	return nil
}
