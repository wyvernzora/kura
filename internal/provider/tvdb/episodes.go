package tvdb

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/provider"
)

type seriesEpisodesResponse struct {
	Data struct {
		Episodes []episodeRecord `json:"episodes"`
	} `json:"data"`
	Status string `json:"status"`
	Links  links  `json:"links"`
}

type episodeRecord struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	Aired          string `json:"aired"`
	FirstAired     string `json:"firstAired"`
	Number         int    `json:"number"`
	SeasonNumber   int    `json:"seasonNumber"`
	AbsoluteNumber int    `json:"absoluteNumber"`
}

type seasonRecord struct {
	ID       int             `json:"id"`
	Number   int             `json:"number"`
	Name     string          `json:"name"`
	Image    string          `json:"image"`
	Year     string          `json:"year"`
	Episodes []episodeRecord `json:"episodes"`
}

func (c *client) seriesEpisodes(ctx context.Context, id string) ([]episodeRecord, error) {
	var episodes []episodeRecord

	// TVDB paginates episode lists. The hard cap prevents a malformed response
	// from causing an unbounded loop while remaining far above realistic series
	// page counts.
	const maxEpisodePages = 100
	for page := range maxEpisodePages {
		values := url.Values{}
		values.Set("page", strconv.Itoa(page))

		var out seriesEpisodesResponse
		path := "/series/" + url.PathEscape(id) + "/episodes/default"
		if err := c.get(ctx, path, values, &out); err != nil {
			return nil, err
		}

		episodes = append(episodes, out.Data.Episodes...)
		if !out.Links.hasNext() {
			break
		}
		if page == maxEpisodePages-1 {
			return nil, fmt.Errorf("%w: episode pagination exceeded %d pages", provider.ErrUnavailable, maxEpisodePages)
		}
	}

	return episodes, nil
}

func normalizeSeasons(seasons []seasonRecord, episodes []episodeRecord) []provider.Season {
	bySeason := map[int][]provider.Episode{}
	for _, episode := range episodes {
		number := episode.SeasonNumber
		bySeason[number] = append(bySeason[number], normalizeEpisodeRecord(episode, number))
	}

	out := make([]provider.Season, 0, len(bySeason))
	seen := map[int]bool{}
	for _, season := range seasons {
		if season.Number < 0 {
			continue
		}
		if seen[season.Number] {
			continue
		}
		seasonEpisodes, ok := bySeason[season.Number]
		if !ok && len(season.Episodes) > 0 {
			seasonEpisodes = normalizeEmbeddedEpisodes(season.Episodes, season.Number)
		}
		if !ok && len(seasonEpisodes) == 0 {
			continue
		}

		out = append(out, provider.Season{
			MetadataRef: providerIntRef(season.ID),
			Number:      season.Number,
			Episodes:    seasonEpisodes,
		})
		seen[season.Number] = true
	}

	for seasonNumber, seasonEpisodes := range bySeason {
		if seen[seasonNumber] {
			continue
		}
		out = append(out, provider.Season{
			MetadataRef: "",
			Number:      seasonNumber,
			Episodes:    seasonEpisodes,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Number < out[j].Number
	})
	for i := range out {
		// Deterministic ordering makes read views and tests stable regardless of
		// TVDB response order.
		sort.Slice(out[i].Episodes, func(j, k int) bool {
			return out[i].Episodes[j].Ref.Episode() < out[i].Episodes[k].Ref.Episode()
		})
	}

	return out
}

func normalizeEmbeddedEpisodes(episodes []episodeRecord, seasonNumber int) []provider.Episode {
	out := make([]provider.Episode, 0, len(episodes))
	for _, episode := range episodes {
		number := firstPositive(episode.SeasonNumber, seasonNumber)
		out = append(out, normalizeEpisodeRecord(episode, number))
	}
	return out
}

func normalizeEpisodeRecord(episode episodeRecord, seasonNumber int) provider.Episode {
	ref, _ := refs.NewEpisode(seasonNumber, episode.Number)
	return provider.Episode{
		MetadataRef:    providerIntRef(episode.ID),
		Ref:            ref,
		AbsoluteNumber: positiveIntPtr(episode.AbsoluteNumber),
		Aired:          firstNormalizedDate(episode.Aired, episode.FirstAired),
	}
}
