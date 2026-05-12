package mcp

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/workflow"
)

type showInput struct {
	Ref        string   `json:"ref" jsonschema:"Metadata ref to inspect (e.g. \"tvdb:370070\"). Get one from kura_resolve."`
	Episodes   string   `json:"episodes,omitempty" jsonschema:"Optional episode selector: S<N> | S<N>E<E> | S<N>E<A>-<B>. Specials = S0. Empty = whole series."`
	Status     []string `json:"status,omitempty" jsonschema:"Optional set of episode statuses to include (pending, missing, present, staged, staged_replacement). Empty = all statuses."`
	Source     []string `json:"source,omitempty" jsonschema:"Optional set of active-media sources to include (BluRay, WebRip, etc.). Empty = all sources."`
	Resolution []string `json:"resolution,omitempty" jsonschema:"Optional set of active-media resolutions to include (1080p, 720p, etc.). Empty = all resolutions."`
}

//go:embed tool_show.md
var toolShowDoc string

// mcpShow is the lean projection of response.Show that this surface
// emits. Fields the agent can't act on (raw series ref, library root)
// are dropped. Active media paths are series-relative slash form so
// agents can pass them back as `series:` selectors; staged paths
// stay as recorded (absolute for inbox stages, series-relative for
// in-place stages) so the agent can verify its own staging actions.
type mcpShow struct {
	MetadataRef     string           `json:"metadataRef"`
	PreferredTitle  string           `json:"preferredTitle"`
	CanonicalTitle  string           `json:"canonicalTitle,omitempty"`
	LastScanned     string           `json:"lastScanned,omitempty"`
	Artwork         *mcpArtwork      `json:"artwork,omitempty"`
	Seasons         []mcpSeason      `json:"seasons"`
	Truncated       bool             `json:"truncated,omitempty"`
	TruncatedRanges []string         `json:"truncatedRanges,omitempty"`
	TruncationHint  string           `json:"truncationHint,omitempty"`
	StagedTrash     []mcpStagedTrash `json:"stagedTrash,omitempty"`
	StagedExtras    []mcpStagedExtra `json:"stagedExtras,omitempty"`
}

type mcpArtwork struct {
	Poster *mcpPoster `json:"poster,omitempty"`
}

type mcpPoster struct {
	URL          string `json:"url"`
	ThumbnailURL string `json:"thumbnailUrl,omitempty"`
	Language     string `json:"language,omitempty"`
}

// mcpStagedTrash is the agent-facing projection of a stagedTrash item.
// Only the original path + size surface; trash bucket structure stays
// backstage. From the agent's POV the path will simply disappear at
// the next reconcile_apply.
type mcpStagedTrash struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type mcpStagedExtra struct {
	ID     string `json:"id"`
	Season int    `json:"season"`
	Path   string `json:"path"`
	Prefix string `json:"prefix,omitempty"`
	IsDir  bool   `json:"isDir"`
}

type mcpSeason struct {
	Number   int              `json:"number"`
	Summary  mcpSeasonSummary `json:"summary"`
	Episodes []mcpEpisode     `json:"episodes,omitempty"`
}

type mcpSeasonSummary struct {
	EpisodeCount      int `json:"episodeCount"`
	Present           int `json:"present"`
	Missing           int `json:"missing"`
	Staged            int `json:"staged"`
	StagedReplacement int `json:"stagedReplacement"`
	Pending           int `json:"pending"`
}

type mcpEpisode struct {
	Episode        string          `json:"episode"`
	Aired          string          `json:"aired,omitempty"`
	Status         string          `json:"status"`
	PreferredTitle string          `json:"preferredTitle,omitempty"`
	CanonicalTitle string          `json:"canonicalTitle,omitempty"`
	Active         *mcpActiveMedia `json:"active,omitempty"`
	Staged         *mcpStagedMedia `json:"staged,omitempty"`
}

type mcpActiveMedia struct {
	Source     string   `json:"source"`
	Resolution string   `json:"resolution,omitempty"`
	Codec      string   `json:"codec,omitempty"`
	Size       int64    `json:"size"`
	File       string   `json:"file"`
	Companions []string `json:"companions"`
}

type mcpStagedMedia struct {
	Source     string   `json:"source"`
	Resolution string   `json:"resolution,omitempty"`
	Codec      string   `json:"codec,omitempty"`
	Size       int64    `json:"size"`
	File       string   `json:"file"`
	Companions []string `json:"companions"`
}

func addShowTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_show",
		Title:       "Show series state",
		Description: forLLM(toolShowDoc),
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    true,
			IdempotentHint:  true,
			OpenWorldHint:   &hintFalse,
			DestructiveHint: &hintFalse,
		},
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in showInput) (*sdkmcp.CallToolResult, any, error) {
		metaRef, err := refs.ParseMetadata(in.Ref)
		if err != nil {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_show: %v", err),
			}), nil, nil
		}
		// Parse the selector at the decode boundary so malformed input
		// rejects before any series load.
		episodes, err := refs.ParseEpisodeSelector(in.Episodes)
		if err != nil {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidEpisode,
				message: fmt.Sprintf("kura_show: episodes: %v", err),
			}), nil, nil
		}
		seriesRef, ok, err := deps.Workflow.Index.Get(metaRef)
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		if !ok {
			return toolErrorResult(&workflow.MetadataRefNotIndexedError{Ref: metaRef}), nil, nil
		}
		statuses := make([]response.Status, 0, len(in.Status))
		for _, s := range in.Status {
			statuses = append(statuses, response.Status(s))
		}
		full, err := workflow.Show(ctx, deps.Workflow, workflow.ShowInput{
			Ref:        seriesRef,
			Episodes:   episodes,
			Status:     statuses,
			Source:     in.Source,
			Resolution: in.Resolution,
		})
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		out := projectShow(full)
		truncateMCPShow(&out, seriesRef.String(), deps.Workflow.Logger)
		return nil, out, nil
	})
}

// mcpShowTruncateBytes is the JSON-bytes ceiling for kura_show MCP
// responses. Set ~20% under typical Claude Code tool-result ceilings
// (~100 KB) to leave headroom for protocol framing.
const mcpShowTruncateBytes = 80 * 1024

// truncateMCPShow drops episode bodies from the tail (in spine order)
// when the projected response exceeds mcpShowTruncateBytes. Per-season
// summaries stay intact. Dropped slots are surfaced as selector-grammar
// strings in TruncatedRanges so the caller can pass any one back as
// `episodes` to fetch its detail.
func truncateMCPShow(out *mcpShow, ref string, log *slog.Logger) {
	raw, err := json.Marshal(out)
	if err != nil || len(raw) <= mcpShowTruncateBytes {
		return
	}

	// Total episodes pre-truncation, for the INFO log.
	totalEpisodes := 0
	for _, season := range out.Seasons {
		totalEpisodes += len(season.Episodes)
	}

	// Drop tail episodes (descending across seasons → episodes within
	// season) until under threshold. Episodes are dropped in batches
	// sized by the current excess / average-episode-bytes estimate, so
	// a 2000-episode show converges in ~5 marshal calls instead of the
	// O(N) one-pop-per-marshal loop the prior implementation did. The
	// estimate is recomputed each iteration as the remaining episode
	// count shrinks, so a too-aggressive first guess self-corrects on
	// the next pass without ever overshooting (we round UP, but the
	// re-marshal verifies before we drop more).
	dropped := map[int][]int{} // season number → dropped episode numbers
	remaining := totalEpisodes
	const maxIterations = 20
	for iter := 0; iter < maxIterations; iter++ {
		raw, err = json.Marshal(out)
		if err != nil {
			return
		}
		if len(raw) <= mcpShowTruncateBytes {
			break
		}
		if remaining == 0 {
			break // nothing left to drop
		}
		excess := len(raw) - mcpShowTruncateBytes
		// avgBytes is the mean serialized size of the episodes still in
		// the payload, with a floor of 1 to avoid div-by-zero when the
		// fixed scaffold (series header, summaries) dominates.
		avgBytes := len(raw) / remaining
		if avgBytes < 1 {
			avgBytes = 1
		}
		// 5% safety margin keeps small estimation errors from forcing a
		// second iteration. Floor of 1 makes sure we always make
		// progress.
		dropCount := excess/avgBytes + excess/(avgBytes*20) + 1
		if dropCount > remaining {
			dropCount = remaining
		}
		dropped, remaining = popTailEpisodes(out, dropped, dropCount, remaining)
	}

	if len(dropped) == 0 {
		return
	}
	out.Truncated = true
	out.TruncatedRanges = compressDroppedRanges(dropped, out.Seasons)
	out.TruncationHint = "Response exceeded the size budget; pass any `truncatedRanges` entry as `episodes` to fetch its detail."

	episodesTruncated := 0
	for _, eps := range dropped {
		episodesTruncated += len(eps)
	}
	if log != nil {
		log.Info("kura_show response truncated",
			"ref", ref,
			"bytesIncluded", len(raw),
			"bytesBudget", mcpShowTruncateBytes,
			"episodesIncluded", totalEpisodes-episodesTruncated,
			"episodesTruncated", episodesTruncated,
			"truncatedRanges", out.TruncatedRanges,
		)
	}
}

// popTailEpisodes drops up to n tail episodes from out, descending
// across seasons (last season first) and within season (last episode
// first). Returns the updated dropped map plus the new remaining
// episode count. Stops early if seasons run out before n is reached.
func popTailEpisodes(out *mcpShow, dropped map[int][]int, n, remaining int) (newDropped map[int][]int, newRemaining int) {
	for n > 0 {
		// Find the last season that still has at least one episode.
		si := len(out.Seasons) - 1
		for si >= 0 && len(out.Seasons[si].Episodes) == 0 {
			si--
		}
		if si < 0 {
			break
		}
		eps := out.Seasons[si].Episodes
		take := n
		if take > len(eps) {
			take = len(eps)
		}
		// Drop `take` episodes from the tail; record their numbers in
		// the dropped map so compressDroppedRanges can rebuild ranges.
		for i := len(eps) - take; i < len(eps); i++ {
			ep, parseErr := refs.ParseEpisode(eps[i].Episode)
			if parseErr == nil {
				dropped[ep.Season()] = append(dropped[ep.Season()], ep.Episode())
			}
		}
		out.Seasons[si].Episodes = eps[:len(eps)-take]
		remaining -= take
		n -= take
	}
	return dropped, remaining
}

// compressDroppedRanges turns a per-season set of dropped episode
// numbers into selector-grammar strings. Dropped numbers come from a
// tail-pop loop so they're contiguous descending; sort + scan gives a
// single inclusive range per truncated season. If the dropped set
// covers the whole season, emit `S<N>` (no episode range).
func compressDroppedRanges(dropped map[int][]int, seasons []mcpSeason) []string {
	totalEpisodesPerSeason := map[int]int{}
	for _, s := range seasons {
		totalEpisodesPerSeason[s.Number] = s.Summary.EpisodeCount
	}
	seasonNums := make([]int, 0, len(dropped))
	for n := range dropped {
		seasonNums = append(seasonNums, n)
	}
	sort.Ints(seasonNums)
	out := make([]string, 0, len(seasonNums))
	for _, sn := range seasonNums {
		eps := dropped[sn]
		sort.Ints(eps)
		first, last := eps[0], eps[len(eps)-1]
		// Whole-season drop → `S<N>`.
		if len(eps) == totalEpisodesPerSeason[sn] {
			sel := refs.EpisodeSelector{Active: true, Season: sn}
			out = append(out, sel.String())
			continue
		}
		sel := refs.EpisodeSelector{Active: true, Season: sn, HasRange: true, From: first, To: last}
		out = append(out, sel.String())
	}
	return out
}

// projectShow strips the operator-only fields and collapses
// staged_replacement into staged.
func projectShow(in response.Show) mcpShow {
	out := mcpShow{
		MetadataRef:    in.MetadataRef.String(),
		PreferredTitle: in.PreferredTitle,
		CanonicalTitle: in.CanonicalTitle,
		LastScanned:    in.LastScanned,
		Seasons:        make([]mcpSeason, 0, len(in.Seasons)),
	}
	if in.Artwork != nil {
		artwork := &mcpArtwork{}
		if in.Artwork.Poster != nil {
			artwork.Poster = &mcpPoster{
				URL:          in.Artwork.Poster.URL,
				ThumbnailURL: in.Artwork.Poster.ThumbnailURL,
				Language:     in.Artwork.Poster.Language,
			}
		}
		out.Artwork = artwork
	}
	for _, season := range in.Seasons {
		eps := make([]mcpEpisode, 0, len(season.Episodes))
		for _, ep := range season.Episodes {
			eps = append(eps, projectEpisode(ep))
		}
		out.Seasons = append(out.Seasons, mcpSeason{
			Number: season.Number,
			Summary: mcpSeasonSummary{
				EpisodeCount:      season.Summary.EpisodeCount,
				Present:           season.Summary.Present,
				Missing:           season.Summary.Missing,
				Staged:            season.Summary.Staged,
				StagedReplacement: season.Summary.StagedReplacement,
				Pending:           season.Summary.Pending,
			},
			Episodes: eps,
		})
	}
	if len(in.StagedTrash) > 0 {
		out.StagedTrash = make([]mcpStagedTrash, 0, len(in.StagedTrash))
		for _, item := range in.StagedTrash {
			out.StagedTrash = append(out.StagedTrash, mcpStagedTrash{
				ID:   item.ID,
				Path: item.Path,
				Size: item.Size,
			})
		}
	}
	if len(in.StagedExtras) > 0 {
		out.StagedExtras = make([]mcpStagedExtra, 0, len(in.StagedExtras))
		for _, item := range in.StagedExtras {
			out.StagedExtras = append(out.StagedExtras, mcpStagedExtra{
				ID:     item.ID,
				Season: item.Season,
				Path:   item.Path,
				Prefix: item.Prefix,
				IsDir:  item.IsDir,
			})
		}
	}
	return out
}

func projectEpisode(ep response.EpisodeShow) mcpEpisode {
	out := mcpEpisode{
		Episode:        ep.Episode.String(),
		Aired:          ep.Aired,
		Status:         collapseStatus(ep.Status),
		PreferredTitle: ep.PreferredTitle,
		CanonicalTitle: ep.CanonicalTitle,
	}
	if ep.Active != nil {
		out.Active = &mcpActiveMedia{
			Source:     ep.Active.Source,
			Resolution: ep.Active.Resolution,
			Codec:      ep.Active.Codec,
			Size:       ep.Active.Size,
			File:       ep.Active.File,
			Companions: companionPaths(ep.Active.Companions),
		}
	}
	if ep.Staged != nil {
		out.Staged = &mcpStagedMedia{
			Source:     ep.Staged.Source,
			Resolution: ep.Staged.Resolution,
			Codec:      ep.Staged.Codec,
			Size:       ep.Staged.Size,
			File:       ep.Staged.File,
			Companions: companionPaths(ep.Staged.Companions),
		}
	}
	return out
}

// collapseStatus maps staged_replacement → staged. The replacement
// nuance is encoded by `active` being present alongside `staged`.
func collapseStatus(s response.Status) string {
	if s == response.StatusStagedReplacement {
		return string(response.StatusStaged)
	}
	return string(s)
}

func companionPaths(in []response.CompanionShow) []string {
	out := make([]string, 0, len(in))
	for _, c := range in {
		out = append(out, c.Path)
	}
	return out
}
