package mcp

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/response"
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
						File:       "/library/Bookworm/Season 1/Bookworm S01E01.mkv",
						Companions: []response.CompanionShow{
							{Path: "/library/Bookworm/Season 1/Bookworm S01E01.en.srt"},
						},
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
	if len(ep.Active.Companions) != 1 || ep.Active.Companions[0] != "Bookworm S01E01.en.srt" {
		t.Fatalf("companion basenames = %v, want [Bookworm S01E01.en.srt]", ep.Active.Companions)
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

func TestProjectEpisode_InconsistenciesDropPath(t *testing.T) {
	got := projectEpisode(response.EpisodeShow{
		Episode: mustEpisode(t, 1, 3),
		Status:  response.StatusUnavailable,
		Inconsistencies: []response.Issue{
			{Record: "active", Path: "/library/x.mkv", Code: "missing_file", Reason: "file not found"},
		},
	})
	if len(got.Inconsistencies) != 1 {
		t.Fatalf("Inconsistencies = %v", got.Inconsistencies)
	}
	got0 := got.Inconsistencies[0]
	if got0.Record != "active" || got0.Code != "missing_file" || got0.Reason != "file not found" {
		t.Fatalf("Inconsistencies[0] = %+v", got0)
	}
}
