//go:build e2e

package e2e

import (
	"context"

	"github.com/wyvernzora/kura/services/library/internal/domain/media"
	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library/internal/provider"
	"github.com/wyvernzora/kura/services/library/internal/textnorm"
)

// stubProvider implements provider.Source using canned in-memory data.
// Key() returns "stub", so callers must use refs.Metadata("stub:<id>").
type stubProvider struct {
	seriesByID map[string]provider.Series
}

// newDefaultStubProvider returns a stub seeded with one series:
//
//	MetadataID:     "1001"
//	PreferredTitle: "Stub Show"
//	Seasons:        1 season, 3 episodes (S01E0001–S01E0003)
func newDefaultStubProvider() *stubProvider {
	ep1, _ := refs.NewEpisode(1, 1)
	ep2, _ := refs.NewEpisode(1, 2)
	ep3, _ := refs.NewEpisode(1, 3)
	return &stubProvider{
		seriesByID: map[string]provider.Series{
			"1001": {
				SeriesSummary: provider.SeriesSummary{
					MetadataRef:    "stub:1001",
					PreferredTitle: textnorm.NFC("Stub Show"),
					CanonicalTitle: textnorm.NFC("Stub Show"),
					Status:         provider.SeriesStatusContinuing,
				},
				Seasons: []provider.Season{
					{
						Number: 1,
						Episodes: []provider.Episode{
							{
								Ref:            ep1,
								Aired:          "2020-01-01",
								PreferredTitle: textnorm.NFC("Pilot"),
								CanonicalTitle: textnorm.NFC("Pilot"),
							},
							{
								Ref:            ep2,
								Aired:          "2020-01-08",
								PreferredTitle: textnorm.NFC("Episode 2"),
								CanonicalTitle: textnorm.NFC("Episode 2"),
							},
							{
								Ref:            ep3,
								Aired:          "2020-01-15",
								PreferredTitle: textnorm.NFC("Episode 3"),
								CanonicalTitle: textnorm.NFC("Episode 3"),
							},
						},
					},
				},
			},
		},
	}
}

func (s *stubProvider) Key() string { return "stub" }

func (s *stubProvider) Search(_ context.Context, _ textnorm.NFCString, _ provider.SearchOptions) ([]provider.SearchResult, error) {
	return nil, nil
}

func (s *stubProvider) GetSeries(_ context.Context, id, _ string) (provider.Series, error) {
	m, ok := s.seriesByID[id]
	if !ok {
		return provider.Series{}, provider.ErrNotFound
	}
	return m, nil
}

// stubInspector implements media.Inspector returning fixed canned facts.
// Used in E2E tests so Stage does not require a real mediainfo binary.
type stubInspector struct {
	resolution string // e.g. "1920x1080"
	videoCodec string // e.g. "H.264"
}

// newDefaultStubInspector returns an inspector that reports 1080p H.264
// for every file, regardless of actual content.
func newDefaultStubInspector() *stubInspector {
	return &stubInspector{resolution: "1920x1080", videoCodec: "H.264"}
}

func (i *stubInspector) Inspect(_ context.Context, _ string) (media.Info, error) {
	return media.Info{
		Resolution: i.resolution,
		VideoCodec: i.videoCodec,
	}, nil
}
