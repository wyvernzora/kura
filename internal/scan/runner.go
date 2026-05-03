package scan

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	domainseries "github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/mediainfo"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/provider"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/seriesdir"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

// Runner bundles the dependencies a single scan needs.
type Runner struct {
	root      string
	ref       refs.Series
	source    provider.Source
	inspector media.Inspector
	now       func() time.Time
}

// NewRunner returns a Runner ready to Scan one series.
func NewRunner(root string, ref refs.Series, source provider.Source, inspector media.Inspector, now func() time.Time) Runner {
	return Runner{
		root:      root,
		ref:       ref,
		source:    source,
		inspector: inspector,
		now:       now,
	}
}

// Scan loads the series, refreshes its spine from the provider, walks
// the filesystem, and reconciles discovered files into the in-memory
// model.
func (r Runner) Scan(ctx context.Context, input Input) (Result, error) {
	s := scanner{Runner: r, input: input, result: Result{Series: r.ref}}
	if err := s.run(ctx); err != nil {
		return Result{}, err
	}
	return s.result, nil
}

// scanner is per-call state. Embeds Runner so methods reach config
// directly (s.root, s.ref, s.now()) instead of through s.runner.X.
type scanner struct {
	Runner
	input     Input
	model     domainseries.Series
	seriesDir seriesdir.SeriesDir
	result    Result
}

func (s *scanner) run(ctx context.Context) (err error) {
	progress.Start(ctx, "scan", fmt.Sprintf("Scanning %s", s.ref), 0)
	defer func() {
		if err != nil {
			progress.Failure(ctx, "scan", fmt.Sprintf("Failed to scan %s", s.ref), 0, 0)
		}
	}()
	if err = s.loadLocal(); err != nil {
		return err
	}
	if err = s.rejectStagedRecords(); err != nil {
		return err
	}
	if err = s.refreshMetadata(ctx); err != nil {
		return err
	}
	progress.Update(ctx, "scan", fmt.Sprintf("Discovering files in %s", s.ref), 1, 0)
	discovered, skipped, err := DiscoverSeriesEpisodes(s.seriesDir)
	if err != nil {
		return err
	}
	s.result.Skipped = skipped
	progress.Update(ctx, "scan", fmt.Sprintf("Inspecting %d files", len(discovered)), 2, 0)
	if err = s.apply(ctx, discovered); err != nil {
		return err
	}
	s.model.LastScanned = s.now().UTC()
	s.model.Ref = s.ref
	if err = seriesfile.Save(s.root, &s.model); err != nil {
		return err
	}
	progress.Success(ctx, "scan", fmt.Sprintf("Scanned %s", s.ref), len(discovered))
	return nil
}

func (s *scanner) loadLocal() error {
	model, err := seriesfile.Load(s.root, s.ref)
	if err != nil {
		return err
	}
	dir, err := seriesdir.Parse(paths.SeriesDir(s.root, s.ref))
	if err != nil {
		return err
	}
	s.model = *model
	s.seriesDir = dir
	return nil
}

func (s *scanner) refreshMetadata(ctx context.Context) error {
	progress.Update(ctx, "scan", fmt.Sprintf("Fetching metadata for %s", s.ref), 0, 0)
	metadataSeries, err := s.source.GetSeries(ctx, s.model.Metadata.ID())
	if err != nil {
		return err
	}
	spine, err := spineFromMetadata(metadataSeries.Seasons)
	if err != nil {
		return err
	}
	s.model.RefreshSpine(spine)
	known := make(map[refs.Episode]struct{}, len(spine))
	for _, entry := range spine {
		known[entry.Ref] = struct{}{}
	}
	orphans := s.model.PruneSpine(known)
	if len(orphans) > 0 {
		sort.Slice(orphans, func(i, j int) bool { return orphans[i].String() < orphans[j].String() })
		s.result.OrphanSlots = orphans
	}
	return nil
}

func (s *scanner) rejectStagedRecords() error {
	var staged []refs.Episode
	for ref, episode := range s.model.Episodes {
		if episode.Staged != nil {
			staged = append(staged, ref)
		}
	}
	if len(staged) == 0 {
		return nil
	}
	sort.Slice(staged, func(i, j int) bool { return staged[i].String() < staged[j].String() })
	return ScanStagedRecordsError{Episodes: staged}
}

func (s *scanner) builder() mediainfo.Builder {
	return mediainfo.NewBuilder(s.inspector)
}

func (s *scanner) absRel(rel string) string {
	return filepath.Join(s.seriesDir.Path(), filepath.FromSlash(rel))
}

func spineFromMetadata(seasons []provider.Season) ([]domainseries.SpineEntry, error) {
	var spine []domainseries.SpineEntry
	for _, season := range seasons {
		for _, episode := range season.Episodes {
			if episode.Ref.IsZero() {
				return nil, fmt.Errorf("series: metadata has invalid episode ref")
			}
			airDate, err := domainseries.ParseAirDate(episode.Aired)
			if err != nil {
				return nil, fmt.Errorf("series: invalid air date %q: %w", episode.Aired, err)
			}
			spine = append(spine, domainseries.SpineEntry{Ref: episode.Ref, AirDate: airDate})
		}
	}
	return spine, nil
}
