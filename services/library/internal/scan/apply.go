package scan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/mediainfo"
	"github.com/wyvernzora/kura/internal/progress"
)

type fileFacts struct {
	Size  int64
	MTime time.Time
}

func statFacts(path string) (fileFacts, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileFacts{}, err
	}
	if info.IsDir() {
		return fileFacts{}, fmt.Errorf("scan: %q is a directory", path)
	}
	return fileFacts{Size: info.Size(), MTime: info.ModTime().UTC().Truncate(time.Second)}, nil
}

func (s *scanner) apply(ctx context.Context, discovered []DiscoveredFile) error {
	if err := s.removeMissingActive(discovered); err != nil {
		return err
	}
	for index, file := range discovered {
		progress.Update(ctx, "scan", fmt.Sprintf("Inspecting %s", filepath.Base(file.Path)), index+1, len(discovered))
		if _, ok := s.model.Episodes[file.Ref]; !ok {
			// Filename parsed cleanly but the provider's spine has no
			// such slot (commonly a multi-cour release split into
			// "Season 2" subdir on disk while the provider treats it
			// as one continuous season). Soft skip; operator decides
			// how to map the file.
			s.result.Skipped = append(s.result.Skipped, ImportSkip{
				Path:   file.Path,
				Code:   SkipCodeMetadataSlotMissing,
				Reason: fmt.Sprintf("metadata has no %s", file.Ref.Marker()),
			})
			continue
		}
		if err := s.applyFile(ctx, file); err != nil {
			return err
		}
	}
	return nil
}

func (s *scanner) applyFile(ctx context.Context, file DiscoveredFile) error {
	episode := s.model.Episodes[file.Ref]
	absolutePath := s.absRel(file.Path)
	status := ScanStatusAdded
	if episode.Active != nil {
		// Different path: scan refuses to silently replace a tracked
		// active record. Caller must explicitly stage the new file.
		if episode.Active.Path != absolutePath {
			return EpisodeAlreadyExistsError{Episode: file.Ref}
		}
		// Same path: skip the rebuild when the file is byte-equal to
		// the recorded fingerprint (size + mtime + companion set),
		// unless --refresh forces a full re-probe.
		unchanged, err := s.unchanged(*episode.Active, file)
		if err != nil {
			return err
		}
		if unchanged && !s.input.Refresh {
			s.result.Synced = append(s.result.Synced, existingScannedEpisode(file, *episode.Active))
			return nil
		}
		status = ScanStatusUpdated
	}
	record, err := s.mediaRecord(ctx, file)
	if err != nil {
		return err
	}
	if episode.Active != nil {
		mergeSourceFromExisting(&record, *episode.Active)
		record.Attrs = media.CloneAttrs(episode.Active.Attrs)
	}
	if err := s.model.SetActive(file.Ref, record); err != nil {
		return err
	}
	s.result.Synced = append(s.result.Synced, scannedEpisode(status, file, record))
	return nil
}

// mergeSourceFromExisting preserves a known-canonical source on the
// prior active record when a re-probed record came back with Unknown.
// Source inference is filename-only and lossy: a file that was once
// correctly labelled "BluRay" by the operator (or by a richer prior
// filename) can degrade to "Unknown" if the filename loses its source
// token. The reverse — Unknown → known — is always allowed since new
// info trumps no info. Other media facts (resolution, codec, size,
// mtime) are authoritative from mediainfo and overwrite freely.
//
// Garbage prior values (e.g. "1920x1080" left over from before the
// InferSourceFromFilename fix that stopped recording resolution
// strings as source) are NOT preserved — media.ParseSource passes
// non-canonical strings through as free-form Source values, so the
// "prior != Unknown" check alone wasn't enough to filter them out.
// We require media.IsKnown so only canonical labels survive.
func mergeSourceFromExisting(record *media.Record, prior media.Record) {
	if record.Source != media.SourceUnknown {
		return
	}
	if prior.Source == media.SourceUnknown {
		return
	}
	if !media.IsKnown(prior.Source.String()) {
		return
	}
	record.Source = prior.Source
}

func (s *scanner) unchanged(active media.Record, file DiscoveredFile) (bool, error) {
	facts, err := statFacts(s.absRel(file.Path))
	if err != nil {
		return false, err
	}
	if active.Size != facts.Size || !active.MTime.Equal(facts.MTime) {
		return false, nil
	}
	if len(active.Companions) != len(file.Companions) {
		return false, nil
	}
	companions := map[string]media.Companion{}
	for _, companion := range active.Companions {
		companions[companion.Path] = companion
	}
	for _, path := range file.Companions {
		companion, ok := companions[s.absRel(path)]
		if !ok {
			return false, nil
		}
		facts, err := statFacts(s.absRel(path))
		if err != nil {
			return false, nil
		}
		if companion.Size != facts.Size || !companion.MTime.Equal(facts.MTime) {
			return false, nil
		}
	}
	return true, nil
}

func (s *scanner) mediaRecord(ctx context.Context, file DiscoveredFile) (media.Record, error) {
	absolutePath := s.absRel(file.Path)
	input := mediainfo.Input{
		MediaPath:  absolutePath,
		RecordPath: absolutePath,
		Source:     file.Source,
	}
	for _, companionPath := range file.Companions {
		absolute := s.absRel(companionPath)
		input.CompanionPaths = append(input.CompanionPaths, mediainfo.CompanionInput{
			MediaPath:  absolute,
			RecordPath: absolute,
		})
	}
	return s.builder().Build(ctx, input)
}

func scannedEpisode(status ScanStatus, file DiscoveredFile, record media.Record) ScannedEpisode {
	return ScannedEpisode{
		Status:     status,
		Episode:    file.Ref,
		Source:     record.Source.Display(),
		Resolution: record.Resolution.String(),
		Path:       record.Path,
		Companions: append([]string(nil), file.Companions...),
	}
}

func existingScannedEpisode(file DiscoveredFile, active media.Record) ScannedEpisode {
	return ScannedEpisode{
		Status:     ScanStatusUnchanged,
		Episode:    file.Ref,
		Source:     active.Source.Display(),
		Resolution: active.Resolution.String(),
		Path:       active.Path,
		Companions: append([]string(nil), file.Companions...),
	}
}

// removeMissingActive prunes active records whose file did not turn up
// in the discovery walk. The walk is the source of truth for what's on
// disk, so a missing entry there means the file is gone.
func (s *scanner) removeMissingActive(discovered []DiscoveredFile) error {
	discoveredPaths := map[string]struct{}{}
	for _, file := range discovered {
		discoveredPaths[s.absRel(file.Path)] = struct{}{}
	}
	refsWithActive := make([]refs.Episode, 0, len(s.model.Episodes))
	for ref, episode := range s.model.Episodes {
		if episode.Active != nil {
			refsWithActive = append(refsWithActive, ref)
		}
	}
	sort.Slice(refsWithActive, func(i, j int) bool {
		if refsWithActive[i].Season() != refsWithActive[j].Season() {
			return refsWithActive[i].Season() < refsWithActive[j].Season()
		}
		return refsWithActive[i].Episode() < refsWithActive[j].Episode()
	})
	for _, ref := range refsWithActive {
		episode := s.model.Episodes[ref]
		if _, ok := discoveredPaths[episode.Active.Path]; ok {
			continue
		}
		record := media.CloneRecord(*episode.Active)
		if err := s.model.ClearActive(ref); err != nil {
			return err
		}
		s.result.Synced = append(s.result.Synced, removedScannedEpisode(ref, record))
	}
	return nil
}

func removedScannedEpisode(ref refs.Episode, record media.Record) ScannedEpisode {
	return ScannedEpisode{
		Status:     ScanStatusRemoved,
		Episode:    ref,
		Source:     record.Source.Display(),
		Resolution: record.Resolution.String(),
		Path:       record.Path,
		Companions: companionPaths(record.Companions),
	}
}

func companionPaths(records []media.Companion) []string {
	out := make([]string, 0, len(records))
	for _, record := range records {
		out = append(out, record.Path)
	}
	return out
}
