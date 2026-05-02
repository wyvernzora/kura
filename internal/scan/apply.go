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
		if err := s.applyFile(ctx, file); err != nil {
			return err
		}
	}
	return nil
}

func (s *scanner) applyFile(ctx context.Context, file DiscoveredFile) error {
	episode, ok := s.model.Episodes[file.Ref]
	if !ok {
		return MetadataMissingEpisodeError{Episode: file.Ref}
	}
	absolutePath := s.absRel(file.Path)
	status := ScanStatusAdded
	if episode.Active != nil {
		if episode.Active.Path != absolutePath {
			if !s.input.Replace {
				return EpisodeAlreadyExistsError{Episode: file.Ref}
			}
			status = ScanStatusReplaced
		} else if unchanged, err := s.unchanged(*episode.Active, file); err != nil {
			return err
		} else if unchanged {
			s.result.Synced = append(s.result.Synced, existingScannedEpisode(file, *episode.Active))
			return nil
		} else {
			status = ScanStatusUpdated
		}
	}
	record, err := s.mediaRecord(ctx, file)
	if err != nil {
		return err
	}
	if err := s.model.SetActive(file.Ref, record); err != nil {
		return err
	}
	s.result.Synced = append(s.result.Synced, scannedEpisode(status, file, record))
	return nil
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
