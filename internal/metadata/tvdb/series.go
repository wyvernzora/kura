package tvdb

import (
	"context"
	"net/url"

	"github.com/wyvernzora/kura/internal/metadata"
)

type seriesExtendedResponse struct {
	Data   seriesExtendedRecord `json:"data"`
	Status string               `json:"status"`
}

type seriesExtendedRecord struct {
	ID                   int            `json:"id"`
	Name                 string         `json:"name"`
	Slug                 string         `json:"slug"`
	Translations         translations   `json:"translations"`
	FirstAired           string         `json:"firstAired"`
	LastAired            string         `json:"lastAired"`
	OriginalCountry      string         `json:"originalCountry"`
	OriginalLanguage     string         `json:"originalLanguage"`
	DefaultSeasonType    int            `json:"defaultSeasonType"`
	Status               statusRecord   `json:"status"`
	Genres               []genreRecord  `json:"genres"`
	RemoteIDs            []remoteID     `json:"remoteIds"`
	Seasons              []seasonRecord `json:"seasons"`
	OverviewTranslations []string       `json:"overviewTranslations"`
}

func (c *client) seriesExtended(ctx context.Context, id string) (seriesExtendedRecord, error) {
	values := url.Values{}
	values.Set("meta", "translations")
	values.Set("short", "true")

	var out seriesExtendedResponse
	if err := c.get(ctx, "/series/"+url.PathEscape(id)+"/extended", values, &out); err != nil {
		return seriesExtendedRecord{}, err
	}
	return out.Data, nil
}

func (p *Provider) normalizeSeries(record seriesExtendedRecord, episodes []episodeRecord) metadata.Series {
	seasons := normalizeSeasons(record.Seasons, episodes)
	series := metadata.Series{
		SeriesSummary: p.normalizeSeriesSummary(seriesSummaryInput{
			ref:              providerIntRef(record.ID),
			canonicalTitle:   record.Name,
			originalLanguage: record.OriginalLanguage,
			originalCountry:  record.OriginalCountry,
			firstAired:       record.FirstAired,
			status:           normalizeStatus(record.Status.Name),
			year:             yearFromDate(record.FirstAired),
			genres:           normalizeGenres(record.Genres),
			linkedRefs:       normalizeRemoteRefs(record.RemoteIDs),
			titles:           seriesTitleCandidates(record),
		}),
		LastAired: normalizeDate(record.LastAired),
		Seasons:   seasons,
	}
	return series
}

func seriesTitleCandidates(record seriesExtendedRecord) []titleCandidate {
	titles := make([]titleCandidate, 0, len(record.Translations.NameTranslations))
	for _, translation := range record.Translations.NameTranslations {
		value := firstNonEmpty(translation.Name, translation.Title)
		if value == "" {
			continue
		}
		titles = append(titles, titleCandidate{
			Language: translation.Language,
			Value:    value,
		})
	}
	return titles
}

func normalizeGenres(genres []genreRecord) []string {
	names := make([]string, 0, len(genres))
	for _, genre := range genres {
		if genre.Name != "" {
			names = append(names, genre.Name)
		}
	}
	return normalizeGenreNames(names)
}

func normalizeGenreNames(genres []string) []string {
	out := make([]string, 0, len(genres))
	for _, genre := range genres {
		if genre != "" {
			out = append(out, genre)
		}
	}
	return out
}
