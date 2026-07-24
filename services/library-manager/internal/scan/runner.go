package scan

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"time"

	"github.com/wyvernzora/kura/services/library-manager/internal/coord"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/media"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	domainseries "github.com/wyvernzora/kura/services/library-manager/internal/domain/series"
	"github.com/wyvernzora/kura/services/library-manager/internal/mediainfo"
	"github.com/wyvernzora/kura/services/library-manager/internal/progress"
	"github.com/wyvernzora/kura/services/library-manager/internal/provider"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/paths"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/seriesdir"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/seriesfile"
)

// Runner bundles the dependencies a single scan needs.
type Runner struct {
	root               string
	ref                refs.Series
	source             provider.Source
	inspector          media.Inspector
	now                func() time.Time
	logger             *slog.Logger
	preferredLanguages []string
}

// NewRunner returns a Runner ready to Scan one series. logger is
// optional (pass nil to discard); when non-nil, scan emits skip
// events at WARN level so operators can grep server logs to find
// series with problems. preferredLanguages is the user's
// configured preferred-languages list (BCP-47 base form) — fed into the
// searchKey fold each scan; empty disables the translation channel.
func NewRunner(root string, ref refs.Series, source provider.Source, inspector media.Inspector, now func() time.Time, logger *slog.Logger, preferredLanguages []string) Runner {
	return Runner{
		root:               root,
		ref:                ref,
		source:             source,
		inspector:          inspector,
		now:                now,
		logger:             logger,
		preferredLanguages: preferredLanguages,
	}
}

func (r Runner) log() *slog.Logger {
	if r.logger != nil {
		return r.logger
	}
	return slog.New(slog.DiscardHandler)
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
	if err := s.loadLocal(); err != nil {
		return err
	}
	if s.model.InProgress != nil {
		return &coord.BusyError{Scope: coord.SeriesScope(s.ref), Holder: *s.model.InProgress}
	}
	if s.input.Ordering != "" {
		s.model.Ordering = s.input.Ordering
	}
	if err := s.refreshMetadata(ctx); err != nil {
		return err
	}
	if s.input.MetadataOnly {
		// Skip filesystem walk + mediainfo. Persist whatever the
		// metadata refresh produced (spine + artwork + searchKey),
		// stamp lastScanned, save. Active records carry forward
		// untouched.
		s.model.LastScanned = s.now().UTC()
		s.model.Ref = s.ref
		if err := seriesfile.SaveCAS(s.root, &s.model, s.input.Mutator); err != nil {
			return err
		}
		s.setResultModel()
		progress.Success(ctx, "scan", fmt.Sprintf("Refreshed metadata for %s", s.ref), 0)
		return nil
	}
	progress.Update(ctx, "scan", fmt.Sprintf("Discovering files in %s", s.ref), 1, 0)
	discovered, skipped, err := s.discoverSeriesEpisodes()
	if err != nil {
		return err
	}
	s.result.Skipped = skipped
	if len(skipped) > 0 {
		log := s.log().With("ref", s.ref.String())
		for _, skip := range skipped {
			log.Warn("scan skipped file",
				"path", skip.Path,
				"code", skip.Code,
				"reason", skip.Reason,
			)
		}
	}
	progress.Update(ctx, "scan", fmt.Sprintf("Inspecting %d files", len(discovered)), 2, 0)
	if err := s.apply(ctx, discovered); err != nil {
		return err
	}
	s.model.LastScanned = s.now().UTC()
	s.model.Ref = s.ref
	if err := seriesfile.SaveCAS(s.root, &s.model, s.input.Mutator); err != nil {
		return err
	}
	s.setResultModel()
	progress.Success(ctx, "scan", fmt.Sprintf("Scanned %s", s.ref), len(discovered))
	return nil
}

func (s *scanner) setResultModel() {
	model := s.model
	s.result.Model = &model
}

func (s *scanner) discoverSeriesEpisodes() ([]DiscoveredFile, []ImportSkip, error) {
	episodes, skipped, err := WalkSeriesEpisodes(s.seriesDir)
	if err != nil {
		return nil, nil, err
	}
	return rejectDuplicateSlots(s.seriesDir, episodes, skipped, func(file DiscoveredFile) bool {
		episode, ok := s.model.Episodes[file.Ref]
		if !ok || episode.Active == nil {
			return false
		}
		return episode.Active.Path == s.absRel(file.Path)
	})
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
	metadataSeries, err := s.source.GetSeries(ctx, s.model.Metadata.ID(), s.model.Ordering)
	if err != nil {
		return err
	}
	// Every field below is provider-derived. Refresh on every scan so
	// upstream edits (canonical retitle, new translation added,
	// artwork swapped, alias added, spine re-numbered) flow into kura
	// without a manual import --force. Operator-level overrides live
	// outside provider data (UserAliases — persisted separately and
	// preserved across rescans).
	s.model.PreferredTitle = metadataSeries.PreferredTitle
	s.model.CanonicalTitle = metadataSeries.CanonicalTitle
	spine, err := spineFromMetadata(metadataSeries.Seasons)
	if err != nil {
		return err
	}
	s.model.RefreshSpine(spine)
	s.model.Artwork = domainseries.Artwork{
		Poster: domainseries.Poster{
			URL:          metadataSeries.Poster.URL,
			ThumbnailURL: metadataSeries.Poster.ThumbnailURL,
			Language:     metadataSeries.Poster.Language,
		},
	}
	// Provider-fresh aliases + translated titles are transient: fold
	// them into searchKey here, never persist. Next scan refreshes
	// both. UserAliases (persisted) carry forward across scans.
	s.model.RecomputeSearchKey(s.preferredLanguages, metadataSeries.Aliases, metadataSeries.TranslatedTitles)
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
			preferred := episode.PreferredTitle
			if preferred.IsZero() {
				preferred = episode.CanonicalTitle
			}
			spine = append(spine, domainseries.SpineEntry{
				Ref:            episode.Ref,
				AirDate:        airDate,
				CanonicalTitle: episode.CanonicalTitle,
				PreferredTitle: preferred,
			})
		}
	}
	return spine, nil
}
