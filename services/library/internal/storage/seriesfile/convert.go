package seriesfile

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/services/library/internal/coord"
	"github.com/wyvernzora/kura/services/library/internal/domain/media"
	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library/internal/domain/series"
	"github.com/wyvernzora/kura/services/library/internal/textnorm"
)

func fromWire(in seriesV3) (*series.Series, error) {
	metadataRef, err := refs.ParseMetadata(in.MetadataRef)
	if err != nil {
		return nil, err
	}
	lastScanned, err := time.Parse(time.RFC3339, in.LastScanned)
	if err != nil {
		return nil, fmt.Errorf("seriesfile: invalid lastScanned %q: %w", in.LastScanned, err)
	}
	dateAdded := lastScanned
	if in.DateAdded != "" {
		parsed, err := time.Parse(time.RFC3339, in.DateAdded)
		if err != nil {
			return nil, fmt.Errorf("seriesfile: invalid dateAdded %q: %w", in.DateAdded, err)
		}
		dateAdded = parsed
	}
	mutator, err := mutatorFromWire(in.LastMutated)
	if err != nil {
		return nil, err
	}
	episodes, err := episodesFromWire(in.Episodes)
	if err != nil {
		return nil, err
	}
	stagedTrash, err := stagedTrashListFromWire(in.StagedTrash)
	if err != nil {
		return nil, err
	}
	stagedExtras, err := stagedExtraListFromWire(in.StagedExtras)
	if err != nil {
		return nil, err
	}
	tags, err := series.NormalizeTags(in.Tags)
	if err != nil {
		return nil, fmt.Errorf("seriesfile: invalid tags: %w", err)
	}
	out := &series.Series{
		Metadata:       metadataRef,
		PreferredTitle: textnorm.NFC(in.PreferredTitle),
		CanonicalTitle: textnorm.NFC(in.CanonicalTitle),
		DateAdded:      dateAdded.UTC(),
		LastScanned:    lastScanned,
		Ordering:       in.Ordering,
		Artwork:        artworkFromWire(in.Artwork),
		Episodes:       episodes,
		StagedTrash:    stagedTrash,
		StagedExtras:   stagedExtras,
		UserAliases:    userAliasesFromWire(in.UserAliases),
		Tags:           tags,
		SearchKey:      in.SearchKey,
		LastMutated:    mutator,
	}
	if in.InProgress != nil {
		holder, err := holderFromWire(*in.InProgress)
		if err != nil {
			return nil, err
		}
		out.InProgress = &holder
	}
	return out, nil
}

// artworkFromWire projects the wire artwork shell into the domain
// shape. Poster stays nullable so series with no provider artwork
// surface as IsZero.
func artworkFromWire(in artworkV2) series.Artwork {
	if in.Poster == nil {
		return series.Artwork{}
	}
	return series.Artwork{
		Poster: series.Poster{
			URL:          in.Poster.URL,
			ThumbnailURL: in.Poster.ThumbnailURL,
			Language:     in.Poster.Language,
		},
	}
}

// episodesFromWire decodes the wire episode map. Returns an empty
// (non-nil) map when the wire side is empty; callers always get a
// usable map without nil-checking.
func episodesFromWire(in map[string]episodeV2) (map[refs.Episode]series.Episode, error) {
	out := make(map[refs.Episode]series.Episode, len(in))
	for key, ep := range in {
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
		out[ref] = series.Episode{
			AirDate:        air,
			PreferredTitle: textnorm.NFC(ep.PreferredTitle),
			CanonicalTitle: textnorm.NFC(ep.CanonicalTitle),
			Active:         active,
			Staged:         staged,
		}
	}
	return out, nil
}

// stagedTrashListFromWire decodes the StagedTrash slice; empty in →
// empty out so callers don't nil-check.
func stagedTrashListFromWire(in []stagedTrashEntryV1) ([]series.StagedTrashItem, error) {
	out := make([]series.StagedTrashItem, 0, len(in))
	for _, entry := range in {
		item, err := stagedTrashFromWire(entry)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

// stagedExtraListFromWire decodes the StagedExtras slice.
func stagedExtraListFromWire(in []stagedExtraEntryV1) ([]series.StagedExtraItem, error) {
	out := make([]series.StagedExtraItem, 0, len(in))
	for _, entry := range in {
		item, err := stagedExtraFromWire(entry)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func holderFromWire(in holderV1) (coord.Holder, error) {
	started, err := time.Parse(time.RFC3339, in.Started)
	if err != nil {
		return coord.Holder{}, fmt.Errorf("seriesfile: invalid in_progress.started %q: %w", in.Started, err)
	}
	return coord.Holder{
		Op:      in.Op,
		Token:   in.Token,
		PID:     in.PID,
		Host:    in.Host,
		Started: started.UTC(),
	}, nil
}

func mutatorFromWire(in mutatorV1) (coord.Mutator, error) {
	at, err := time.Parse(time.RFC3339, in.At)
	if err != nil {
		return coord.Mutator{}, fmt.Errorf("seriesfile: invalid last_mutated.at %q: %w", in.At, err)
	}
	return coord.Mutator{
		Op:   in.Op,
		PID:  in.PID,
		Host: in.Host,
		At:   at.UTC(),
	}, nil
}

func holderToWire(in coord.Holder) holderV1 {
	return holderV1{
		Op:      in.Op,
		Token:   in.Token,
		PID:     in.PID,
		Host:    in.Host,
		Started: in.Started.UTC().Format(time.RFC3339),
	}
}

func mutatorToWire(in coord.Mutator) mutatorV1 {
	return mutatorV1{
		Op:   in.Op,
		PID:  in.PID,
		Host: in.Host,
		At:   in.At.UTC().Format(time.RFC3339),
	}
}

func toWire(in *series.Series) (seriesV3, error) {
	tags, err := series.NormalizeTags(in.Tags)
	if err != nil {
		return seriesV3{}, fmt.Errorf("seriesfile: invalid tags: %w", err)
	}
	dateAdded := in.DateAdded
	if dateAdded.IsZero() {
		dateAdded = in.LastScanned
	}
	out := seriesV3{
		SchemaVersion:  currentSchemaVersion,
		MetadataRef:    in.Metadata.String(),
		PreferredTitle: in.PreferredTitle.String(),
		CanonicalTitle: in.CanonicalTitle.String(),
		DateAdded:      dateAdded.UTC().Format(time.RFC3339),
		LastScanned:    in.LastScanned.UTC().Format(time.RFC3339),
		Ordering:       in.Ordering,
		Artwork:        artworkToWire(in.Artwork),
		Episodes:       episodesToWire(in.Episodes),
		StagedTrash:    stagedTrashListToWire(in.StagedTrash),
		StagedExtras:   stagedExtraListToWire(in.StagedExtras),
		UserAliases:    userAliasesToWire(in.UserAliases),
		Tags:           tags,
		SearchKey:      in.SearchKey,
		LastMutated:    mutatorToWire(in.LastMutated),
	}
	if in.InProgress != nil {
		wire := holderToWire(*in.InProgress)
		out.InProgress = &wire
	}
	return out, nil
}

// artworkToWire projects the domain artwork shape into the wire shell.
// Always returns a value (artwork is required on wire); the inner
// poster stays nullable.
func artworkToWire(in series.Artwork) artworkV2 {
	if in.Poster.IsZero() {
		return artworkV2{}
	}
	return artworkV2{
		Poster: &posterV2{
			URL:          in.Poster.URL,
			ThumbnailURL: in.Poster.ThumbnailURL,
			Language:     in.Poster.Language,
		},
	}
}

// episodesToWire emits the spine map in episode-ref-sorted key order
// so the JSON-encoded form is byte-stable across runs.
func episodesToWire(in map[refs.Episode]series.Episode) map[string]episodeV2 {
	out := make(map[string]episodeV2, len(in))
	keys := make([]refs.Episode, 0, len(in))
	for r := range in {
		keys = append(keys, r)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	for _, ref := range keys {
		ep := in[ref]
		var air string
		if ep.AirDate.IsValid() {
			air = ep.AirDate.String()
		}
		out[ref.String()] = episodeV2{
			AirDate:        air,
			PreferredTitle: ep.PreferredTitle.String(),
			CanonicalTitle: ep.CanonicalTitle.String(),
			Active:         mediaToWire(ep.Active),
			Staged:         mediaToWire(ep.Staged),
		}
	}
	return out
}

func stagedTrashListToWire(in []series.StagedTrashItem) []stagedTrashEntryV1 {
	out := make([]stagedTrashEntryV1, 0, len(in))
	for _, item := range in {
		out = append(out, stagedTrashToWire(item))
	}
	return out
}

func stagedExtraListToWire(in []series.StagedExtraItem) []stagedExtraEntryV1 {
	out := make([]stagedExtraEntryV1, 0, len(in))
	for _, item := range in {
		out = append(out, stagedExtraToWire(item))
	}
	return out
}

func stagedTrashFromWire(in stagedTrashEntryV1) (series.StagedTrashItem, error) {
	id, err := ulid.Parse(in.ID)
	if err != nil {
		return series.StagedTrashItem{}, fmt.Errorf("seriesfile: invalid stagedTrash.id %q: %w", in.ID, err)
	}
	out := series.StagedTrashItem{
		ID:         id,
		Path:       in.Path,
		Size:       in.Size,
		Companions: make([]media.Companion, 0, len(in.Companions)),
	}
	if in.MTime != "" {
		mtime, err := time.Parse(time.RFC3339, in.MTime)
		if err != nil {
			return series.StagedTrashItem{}, fmt.Errorf("seriesfile: invalid stagedTrash.mtime %q: %w", in.MTime, err)
		}
		out.MTime = mtime
	}
	if in.AddedAt != "" {
		addedAt, err := time.Parse(time.RFC3339, in.AddedAt)
		if err != nil {
			return series.StagedTrashItem{}, fmt.Errorf("seriesfile: invalid stagedTrash.addedAt %q: %w", in.AddedAt, err)
		}
		out.AddedAt = addedAt
	}
	for _, c := range in.Companions {
		out.Companions = append(out.Companions, companionFromWire(c))
	}
	return out, nil
}

func stagedTrashToWire(in series.StagedTrashItem) stagedTrashEntryV1 {
	out := stagedTrashEntryV1{
		ID:         in.ID.String(),
		Path:       in.Path,
		Size:       in.Size,
		MTime:      in.MTime.UTC().Format(time.RFC3339),
		AddedAt:    in.AddedAt.UTC().Format(time.RFC3339),
		Companions: make([]companionRecordV1, 0, len(in.Companions)),
	}
	for _, c := range in.Companions {
		out.Companions = append(out.Companions, companionToWire(c))
	}
	return out
}

func stagedExtraFromWire(in stagedExtraEntryV1) (series.StagedExtraItem, error) {
	id, err := ulid.Parse(in.ID)
	if err != nil {
		return series.StagedExtraItem{}, fmt.Errorf("seriesfile: invalid stagedExtra.id %q: %w", in.ID, err)
	}
	out := series.StagedExtraItem{
		ID:     id,
		Season: in.Season,
		Path:   in.Path,
		Prefix: in.Prefix,
		IsDir:  in.IsDir,
	}
	if in.AddedAt != "" {
		addedAt, err := time.Parse(time.RFC3339, in.AddedAt)
		if err != nil {
			return series.StagedExtraItem{}, fmt.Errorf("seriesfile: invalid stagedExtra.addedAt %q: %w", in.AddedAt, err)
		}
		out.AddedAt = addedAt
	}
	return out, nil
}

func stagedExtraToWire(in series.StagedExtraItem) stagedExtraEntryV1 {
	return stagedExtraEntryV1{
		ID:      in.ID.String(),
		Season:  in.Season,
		Path:    in.Path,
		Prefix:  in.Prefix,
		IsDir:   in.IsDir,
		AddedAt: in.AddedAt.UTC().Format(time.RFC3339),
	}
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
		Attrs:      media.CloneAttrs(media.Attrs(in.Attrs)),
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
		Attrs:      media.CloneAttrs(in.Attrs),
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
// (episode) paths live outside the series dir and are already absolute on
// disk. StagedTrash paths live under series dir (must be) — absolutized
// here. StagedExtras paths are inbox: selectors — stored verbatim, no join.
func absolutizeActive(s *series.Series, seriesDir string) {
	for ref, ep := range s.Episodes {
		if ep.Active != nil {
			absolutizeRecord(ep.Active, seriesDir)
			s.Episodes[ref] = ep
		}
	}
	for i := range s.StagedTrash {
		if !filepath.IsAbs(s.StagedTrash[i].Path) {
			s.StagedTrash[i].Path = filepath.Join(seriesDir, filepath.FromSlash(s.StagedTrash[i].Path))
		}
		for j, c := range s.StagedTrash[i].Companions {
			if !filepath.IsAbs(c.Path) {
				s.StagedTrash[i].Companions[j].Path = filepath.Join(seriesDir, filepath.FromSlash(c.Path))
			}
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
// (episode) paths stay absolute. StagedTrash paths are relativized to series
// dir; StagedExtras paths are inbox: selectors stored verbatim.
func userAliasesFromWire(in []string) []textnorm.NFCString {
	if len(in) == 0 {
		return nil
	}
	out := make([]textnorm.NFCString, 0, len(in))
	for _, value := range in {
		out = append(out, textnorm.NFC(value))
	}
	return out
}

func userAliasesToWire(in []textnorm.NFCString) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, value := range in {
		out = append(out, value.String())
	}
	return out
}

func relativizeActiveWire(w *seriesV3, seriesDir string) error {
	for key, ep := range w.Episodes {
		if ep.Active != nil {
			if err := relativizeWireRecord(ep.Active, seriesDir); err != nil {
				return err
			}
			w.Episodes[key] = ep
		}
	}
	for i := range w.StagedTrash {
		if filepath.IsAbs(w.StagedTrash[i].Path) {
			rel, err := filepath.Rel(seriesDir, w.StagedTrash[i].Path)
			if err != nil {
				return err
			}
			w.StagedTrash[i].Path = filepath.ToSlash(rel)
		}
		for j, c := range w.StagedTrash[i].Companions {
			if filepath.IsAbs(c.Path) {
				rel, err := filepath.Rel(seriesDir, c.Path)
				if err != nil {
					return err
				}
				w.StagedTrash[i].Companions[j].Path = filepath.ToSlash(rel)
			}
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
