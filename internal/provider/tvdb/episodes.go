package tvdb

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/provider"
	"github.com/wyvernzora/kura/internal/textnorm"
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

// seriesEpisodes fetches the episode spine for a series under a chosen
// ordering. ordering is one of TVDB's season-types ("default", "official",
// "dvd", "absolute", "alternate", "regional"); empty defaults to "default".
// Validation is the caller's responsibility — invalid values yield a TVDB
// 4xx, which surfaces as a non-nil error from c.get.
func (c *client) seriesEpisodes(ctx context.Context, id, ordering string) ([]episodeRecord, error) {
	if ordering == "" {
		ordering = "default"
	}
	var episodes []episodeRecord

	// TVDB paginates episode lists. The hard cap prevents a malformed response
	// from causing an unbounded loop while remaining far above realistic series
	// page counts.
	const maxEpisodePages = 100
	for page := range maxEpisodePages {
		values := url.Values{}
		values.Set("page", strconv.Itoa(page))

		var out seriesEpisodesResponse
		path := "/series/" + url.PathEscape(id) + "/episodes/" + url.PathEscape(ordering)
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

// seriesEpisodesInLanguage fetches the episode spine in a specific
// language. Endpoint: /series/{id}/episodes/{ordering}/{lang}. Used
// to merge preferred-language episode names into the normalized
// view; default-language names already ride the seriesEpisodes call.
func (c *client) seriesEpisodesInLanguage(ctx context.Context, id, ordering, lang string) ([]episodeRecord, error) {
	if ordering == "" {
		ordering = "default"
	}
	if lang == "" {
		return nil, fmt.Errorf("tvdb: seriesEpisodesInLanguage requires a language")
	}
	var episodes []episodeRecord
	const maxEpisodePages = 100
	for page := range maxEpisodePages {
		values := url.Values{}
		values.Set("page", strconv.Itoa(page))
		var out seriesEpisodesResponse
		path := "/series/" + url.PathEscape(id) + "/episodes/" + url.PathEscape(ordering) + "/" + url.PathEscape(lang)
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

func normalizeSeasons(seasons []seasonRecord, episodes []episodeRecord, preferredByID map[int]string) []provider.Season {
	bySeason := map[int][]provider.Episode{}
	for _, episode := range episodes {
		number := episode.SeasonNumber
		bySeason[number] = append(bySeason[number], normalizeEpisodeRecord(episode, number, preferredByID))
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
			seasonEpisodes = normalizeEmbeddedEpisodes(season.Episodes, season.Number, preferredByID)
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

func normalizeEmbeddedEpisodes(episodes []episodeRecord, seasonNumber int, preferredByID map[int]string) []provider.Episode {
	out := make([]provider.Episode, 0, len(episodes))
	for _, episode := range episodes {
		number := firstPositive(episode.SeasonNumber, seasonNumber)
		out = append(out, normalizeEpisodeRecord(episode, number, preferredByID))
	}
	return out
}

func normalizeEpisodeRecord(episode episodeRecord, seasonNumber int, preferredByID map[int]string) provider.Episode {
	ref, _ := refs.NewEpisode(seasonNumber, episode.Number)
	out := provider.Episode{
		MetadataRef:    providerIntRef(episode.ID),
		Ref:            ref,
		AbsoluteNumber: positiveIntPtr(episode.AbsoluteNumber),
		Aired:          firstNormalizedDate(episode.Aired, episode.FirstAired),
		CanonicalTitle: textnorm.NFC(episode.Name),
	}
	if preferredByID != nil {
		if name, ok := preferredByID[episode.ID]; ok && name != "" {
			out.PreferredTitle = textnorm.NFC(name)
		}
	}
	return out
}
