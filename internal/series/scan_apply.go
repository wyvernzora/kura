package series

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/wyvernzora/kura/internal/refs"
)

func (s *scanner) apply(discovered []discoveredFile) error {
	if err := s.removeMissingActive(discovered); err != nil {
		return err
	}
	for index, file := range discovered {
		s.reportInspecting(file, index+1, len(discovered))
		if err := s.applyFile(file); err != nil {
			return err
		}
	}
	return nil
}

func (s *scanner) applyFile(file discoveredFile) error {
	episode, ok := s.model.Episodes[file.Ref]
	if !ok {
		return MetadataMissingEpisodeError{Episode: file.Ref}
	}
	status := ScanStatusAdded
	if episode.Active != nil {
		if episode.Active.Path != file.Path {
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
	record, err := s.mediaRecord(file)
	if err != nil {
		return err
	}
	if err := s.editor.setActive(file.Ref, record); err != nil {
		return err
	}
	s.result.Synced = append(s.result.Synced, scannedEpisode(status, file, record))
	return nil
}

func (s *scanner) unchanged(active MediaRecord, file discoveredFile) (bool, error) {
	facts, err := s.handle.files().stat(filepath.Join(s.seriesDir.Path(), filepath.FromSlash(file.Path)))
	if err != nil {
		return false, err
	}
	if active.Size != facts.Size || !active.MTime.Equal(facts.MTime) {
		return false, nil
	}
	if len(active.Companions) != len(file.Companions) {
		return false, nil
	}
	companions := map[string]CompanionRecord{}
	for _, companion := range active.Companions {
		companions[companion.Path] = companion
	}
	for _, path := range file.Companions {
		companion, ok := companions[path]
		if !ok {
			return false, nil
		}
		facts, err := s.handle.files().stat(filepath.Join(s.seriesDir.Path(), filepath.FromSlash(path)))
		if err != nil {
			return false, nil
		}
		if companion.Size != facts.Size || !companion.MTime.Equal(facts.MTime) {
			return false, nil
		}
	}
	return true, nil
}

func (s *scanner) mediaRecord(file discoveredFile) (MediaRecord, error) {
	absolutePath := filepath.Join(s.seriesDir.Path(), filepath.FromSlash(file.Path))
	info, err := s.handle.inspector().Inspect(s.ctx, absolutePath)
	if err != nil {
		return MediaRecord{}, err
	}
	facts, err := s.handle.files().stat(absolutePath)
	if err != nil {
		return MediaRecord{}, err
	}
	record := MediaRecord{
		Path:       file.Path,
		Source:     ParseMediaSource(file.Source).String(),
		Resolution: info.Resolution,
		Codec:      info.VideoCodec,
		Size:       facts.Size,
		MTime:      facts.MTime,
		Companions: []CompanionRecord{},
	}
	for _, companionPath := range file.Companions {
		facts, err := s.handle.files().stat(filepath.Join(s.seriesDir.Path(), filepath.FromSlash(companionPath)))
		if err != nil {
			return MediaRecord{}, err
		}
		record.Companions = append(record.Companions, CompanionRecord{
			Path:  companionPath,
			Size:  facts.Size,
			MTime: facts.MTime,
		})
	}
	return record, nil
}

func scannedEpisode(status ScanStatus, file discoveredFile, record MediaRecord) ScannedEpisode {
	return ScannedEpisode{
		Status:     status,
		Episode:    file.Ref,
		Source:     ParseMediaSource(record.Source).Display(),
		Resolution: record.Resolution,
		Path:       record.Path,
		Companions: append([]string(nil), file.Companions...),
	}
}

func existingScannedEpisode(file discoveredFile, active MediaRecord) ScannedEpisode {
	return ScannedEpisode{
		Status:     ScanStatusUnchanged,
		Episode:    file.Ref,
		Source:     ParseMediaSource(active.Source).Display(),
		Resolution: active.Resolution,
		Path:       active.Path,
		Companions: append([]string(nil), file.Companions...),
	}
}

func (s *scanner) removeMissingActive(discovered []discoveredFile) error {
	discoveredPaths := map[string]struct{}{}
	for _, file := range discovered {
		discoveredPaths[file.Path] = struct{}{}
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
		path := filepath.Join(s.seriesDir.Path(), filepath.FromSlash(episode.Active.Path))
		if _, err := s.handle.files().stat(path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		record := cloneMediaRecord(*episode.Active)
		if err := s.editor.clearActive(ref); err != nil {
			return err
		}
		s.result.Synced = append(s.result.Synced, removedScannedEpisode(ref, record))
	}
	return nil
}

func removedScannedEpisode(ref refs.Episode, record MediaRecord) ScannedEpisode {
	return ScannedEpisode{
		Status:     ScanStatusRemoved,
		Episode:    ref,
		Source:     ParseMediaSource(record.Source).Display(),
		Resolution: record.Resolution,
		Path:       record.Path,
		Companions: companionPaths(record.Companions),
	}
}

func companionPaths(records []CompanionRecord) []string {
	out := make([]string, 0, len(records))
	for _, record := range records {
		out = append(out, record.Path)
	}
	return out
}
