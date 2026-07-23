package mcp

import (
	"context"
	"encoding/json"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/errkind"
	"github.com/wyvernzora/kura/services/library-manager/internal/response"
)

func mustEpisode(t *testing.T, season, episode int) refs.Episode {
	t.Helper()
	ep, err := refs.NewEpisode(season, episode)
	if err != nil {
		t.Fatalf("NewEpisode(%d,%d): %v", season, episode, err)
	}
	return ep
}

func connectInMemoryWithShow(t *testing.T) *sdkmcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	addShowTool(server, Deps{})
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0"}, nil)
	st, ct := sdkmcp.NewInMemoryTransports()
	srvSession, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { srvSession.Close() })
	clientSession, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { clientSession.Close() })
	return clientSession
}

func TestKuraShow_ToolIsRegistered(t *testing.T) {
	cs := connectInMemoryWithShow(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) != 1 || res.Tools[0].Name != "kura_show" {
		t.Fatalf("unexpected tool list: %+v", res.Tools)
	}
}

func TestKuraShow_InvalidRefRejected(t *testing.T) {
	cs := connectInMemoryWithShow(t)
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "kura_show",
		Arguments: map[string]any{"ref": "not-a-ref"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true")
	}
	body, err := decodeStructured(res.StructuredContent)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["kind"] != errkind.KindInvalidRef {
		t.Fatalf("kind = %v, want %v", body["kind"], errkind.KindInvalidRef)
	}
}

func TestKuraShow_RejectsMalformedEpisodesAtBoundary(t *testing.T) {
	cs := connectInMemoryWithShow(t)
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "kura_show",
		Arguments: map[string]any{
			"ref":      "tvdb:1",
			"episodes": "garbage-not-a-selector",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true")
	}
	body, err := decodeStructured(res.StructuredContent)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["kind"] != errkind.KindInvalidEpisode {
		t.Fatalf("kind = %v, want %v", body["kind"], errkind.KindInvalidEpisode)
	}
}

func TestProjectShow_DropsOperatorFields(t *testing.T) {
	in := response.Show{
		MetadataRef:    refs.Metadata("tvdb:1"),
		Ref:            mustSeries(t, "Bookworm"),
		Root:           "/library/Bookworm",
		LastScanned:    "2026-04-12T08:31:00Z",
		PreferredTitle: "Bookworm",
		Seasons: []response.SeasonShow{
			{Number: 1, Episodes: []response.EpisodeShow{
				{
					Episode: mustEpisode(t, 1, 1),
					Aired:   "2019-10-03",
					Status:  response.StatusPresent,
					Active: &response.MediaShow{
						Source:     "BDRip",
						Resolution: "1080p",
						Size:       1024,
						File:       "series:Season 1/Bookworm S01E01.mkv",
						Companions: []response.CompanionShow{
							{Path: "series:Season 1/Bookworm S01E01.en.srt"},
						},
						Attrs: map[string]string{"origin": "takuhai"},
					},
				},
			}},
		},
	}
	out := projectShow(in)
	if out.MetadataRef != "tvdb:1" {
		t.Fatalf("MetadataRef = %q", out.MetadataRef)
	}
	if len(out.Seasons) != 1 || len(out.Seasons[0].Episodes) != 1 {
		t.Fatalf("unexpected seasons: %+v", out.Seasons)
	}
	ep := out.Seasons[0].Episodes[0]
	if ep.Active == nil {
		t.Fatal("Active dropped")
	}
	if ep.Active.Source != "BDRip" || ep.Active.Resolution != "1080p" {
		t.Fatalf("Active quality lost: %+v", ep.Active)
	}
	if ep.Active.File != "series:Season 1/Bookworm S01E01.mkv" {
		t.Fatalf("Active.File = %q, want series:Season 1/Bookworm S01E01.mkv", ep.Active.File)
	}
	if len(ep.Active.Companions) != 1 || ep.Active.Companions[0] != "series:Season 1/Bookworm S01E01.en.srt" {
		t.Fatalf("companion paths = %v, want [series:Season 1/Bookworm S01E01.en.srt]", ep.Active.Companions)
	}
	if ep.Active.Attrs["origin"] != "takuhai" {
		t.Fatalf("attrs = %#v", ep.Active.Attrs)
	}
}

func TestProjectEpisode_StagedReplacementCollapsesToStaged(t *testing.T) {
	got := projectEpisode(response.EpisodeShow{
		Episode: mustEpisode(t, 1, 2),
		Status:  response.StatusStagedReplacement,
		Active:  &response.MediaShow{Source: "WebRip", Size: 100},
		Staged: &response.MediaShow{
			Source: "BDRip", Size: 200,
			File: "/staging/x.mkv",
			Companions: []response.CompanionShow{
				{Path: "/staging/x.en.srt"},
			},
		},
	})
	if got.Status != "staged" {
		t.Fatalf("Status = %q, want staged", got.Status)
	}
	if got.Active == nil || got.Staged == nil {
		t.Fatal("expected both Active and Staged blocks")
	}
	if got.Staged.File != "/staging/x.mkv" {
		t.Fatalf("Staged.File = %q, want absolute /staging/x.mkv", got.Staged.File)
	}
	if len(got.Staged.Companions) != 1 || got.Staged.Companions[0] != "/staging/x.en.srt" {
		t.Fatalf("Staged.Companions = %v, want [/staging/x.en.srt]", got.Staged.Companions)
	}
}

func TestProjectShow_StagedReplacementSummaryCollapsesToStaged(t *testing.T) {
	got := projectShow(response.Show{
		MetadataRef:    refs.Metadata("tvdb:370070"),
		PreferredTitle: "Bookworm",
		Seasons: []response.SeasonShow{{
			Number: 1,
			Summary: response.SeasonSummary{
				EpisodeCount:      2,
				Staged:            1,
				StagedReplacement: 1,
			},
		}},
	})
	if got.Seasons[0].Summary.Staged != 2 {
		t.Fatalf("Staged = %d, want 2", got.Seasons[0].Summary.Staged)
	}
}

func TestTruncateMCPShow_DropsTailOverBudget(t *testing.T) {
	// Synthesize a large response: 5 seasons × 400 episodes each =
	// 2000 episodes with verbose titles to force a JSON > 80 KB.
	out := mcpShow{
		MetadataRef:    "tvdb:1",
		PreferredTitle: "BigShow",
	}
	const seasonCount = 5
	const epsPerSeason = 400
	for s := 1; s <= seasonCount; s++ {
		season := mcpSeason{Number: s, Summary: mcpSeasonSummary{EpisodeCount: epsPerSeason, Present: epsPerSeason}}
		for e := 1; e <= epsPerSeason; e++ {
			ref, _ := refs.NewEpisode(s, e)
			season.Episodes = append(season.Episodes, mcpEpisode{
				Episode:        ref.String(),
				Aired:          "2020-01-01",
				Status:         "present",
				PreferredTitle: "本好きのお姫様 — A Long Episode Title in Japanese with Padding",
				CanonicalTitle: "The Bookworm Princess — A Long Episode Title in English with Padding",
				Active: &mcpActiveMedia{
					Source: "BluRay", Resolution: "1080p", Codec: "HEVC", Size: 1234567890,
					Companions: []string{"sub.en.srt", "sub.ja.srt"},
				},
			})
		}
		out.Seasons = append(out.Seasons, season)
	}

	truncateMCPShow(&out, "Test", nil)

	if !out.Truncated {
		t.Fatal("expected truncated=true on oversized payload")
	}
	if len(out.TruncatedRanges) == 0 {
		t.Fatal("expected truncatedRanges to be non-empty")
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(raw) > mcpShowTruncateBytes {
		t.Errorf("post-truncate size %d > budget %d", len(raw), mcpShowTruncateBytes)
	}
	// Truncated ranges must be valid selector strings parseable by refs.
	for _, r := range out.TruncatedRanges {
		if _, err := refs.ParseEpisodeSelector(r); err != nil {
			t.Errorf("truncatedRange %q is not a valid selector: %v", r, err)
		}
	}
	// Per-season summaries preserved across the board.
	for _, s := range out.Seasons {
		if s.Summary.EpisodeCount != epsPerSeason {
			t.Errorf("season %d summary lost: %+v", s.Number, s.Summary)
		}
	}
}

func TestTruncateMCPShow_NoOpUnderBudget(t *testing.T) {
	out := mcpShow{
		MetadataRef:    "tvdb:1",
		PreferredTitle: "SmallShow",
		Seasons: []mcpSeason{{
			Number:  1,
			Summary: mcpSeasonSummary{EpisodeCount: 1, Present: 1},
			Episodes: []mcpEpisode{{
				Episode: "S01E0001", Status: "present",
			}},
		}},
	}
	truncateMCPShow(&out, "Test", nil)
	if out.Truncated {
		t.Fatal("small response unexpectedly truncated")
	}
	if len(out.TruncatedRanges) != 0 {
		t.Errorf("expected empty truncatedRanges, got %v", out.TruncatedRanges)
	}
}

func TestProjectShow_PosterAndEpisodeTitles(t *testing.T) {
	in := response.Show{
		MetadataRef:    refs.Metadata("tvdb:1"),
		Ref:            mustSeries(t, "Bookworm"),
		Root:           "/library/Bookworm",
		PreferredTitle: "Bookworm",
		Artwork: &response.ArtworkShow{
			Poster: &response.PosterShow{
				URL:          "https://artworks.thetvdb.com/poster.jpg",
				ThumbnailURL: "https://artworks.thetvdb.com/poster_thumb.jpg",
				Language:     "ja",
			},
		},
		Seasons: []response.SeasonShow{{
			Number: 1,
			Episodes: []response.EpisodeShow{{
				Episode:        mustEpisode(t, 1, 1),
				Status:         response.StatusMissing,
				PreferredTitle: "本好きのお姫様",
				CanonicalTitle: "The Bookworm Princess",
			}},
		}},
	}
	out := projectShow(in)
	if out.Artwork == nil || out.Artwork.Poster == nil {
		t.Fatal("Artwork.Poster dropped")
	}
	if out.Artwork.Poster.URL != "https://artworks.thetvdb.com/poster.jpg" {
		t.Errorf("Poster.URL = %q", out.Artwork.Poster.URL)
	}
	if len(out.Seasons) != 1 || len(out.Seasons[0].Episodes) != 1 {
		t.Fatal("season/episode dropped")
	}
	ep := out.Seasons[0].Episodes[0]
	if ep.PreferredTitle != "本好きのお姫様" {
		t.Errorf("PreferredTitle = %q", ep.PreferredTitle)
	}
	if ep.CanonicalTitle != "The Bookworm Princess" {
		t.Errorf("CanonicalTitle = %q", ep.CanonicalTitle)
	}
}

func TestProjectShow_StagedTrashAndExtras(t *testing.T) {
	in := response.Show{
		MetadataRef:    refs.Metadata("tvdb:1"),
		Ref:            mustSeries(t, "Bookworm"),
		Root:           "/library/Bookworm",
		PreferredTitle: "Bookworm",
		StagedTrash: []response.TrashItemShow{{
			ID:    "01H0000000000000000000AAAA",
			Path:  "Season 1/loser.mkv",
			Size:  1024,
			MTime: "2026-04-12T08:31:00Z",
			Companions: []response.CompanionShow{
				{Path: "Season 1/loser.en.srt", Size: 10},
			},
		}},
		StagedExtras: []response.ExtraItemShow{{
			ID:     "01H0000000000000000000BBBB",
			Season: 1,
			Path:   "/inbox/bts",
			IsDir:  true,
		}},
	}
	out := projectShow(in)
	if len(out.StagedTrash) != 1 {
		t.Fatalf("StagedTrash len = %d, want 1", len(out.StagedTrash))
	}
	tr := out.StagedTrash[0]
	if tr.ID != "01H0000000000000000000AAAA" || tr.Path != "Season 1/loser.mkv" || tr.Size != 1024 {
		t.Fatalf("StagedTrash[0] = %+v", tr)
	}
	// Reflect-style: confirm no companion field on the wire shape.
	// Encoding tested via json marshal in tool_reconcile_plan_test; here
	// we just rely on struct shape (compiler enforces no Companions).
	if len(out.StagedExtras) != 1 {
		t.Fatalf("StagedExtras len = %d, want 1", len(out.StagedExtras))
	}
	ex := out.StagedExtras[0]
	if ex.ID != "01H0000000000000000000BBBB" || ex.Season != 1 || ex.Path != "/inbox/bts" || !ex.IsDir {
		t.Fatalf("StagedExtras[0] = %+v", ex)
	}
}
