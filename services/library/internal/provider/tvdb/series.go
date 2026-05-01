package tvdb

import (
	"context"
	"net/url"

	"github.com/wyvernzora/kura/internal/provider"
)

type seriesExtendedResponse struct {
	Data   seriesExtendedRecord `json:"data"`
	Status string               `json:"status"`
}

type seriesExtendedRecord struct {
	ID                   int             `json:"id"`
	Name                 string          `json:"name"`
	Slug                 string          `json:"slug"`
	Translations         translations    `json:"translations"`
	FirstAired           string          `json:"firstAired"`
	LastAired            string          `json:"lastAired"`
	OriginalCountry      string          `json:"originalCountry"`
	OriginalLanguage     string          `json:"originalLanguage"`
	DefaultSeasonType    int             `json:"defaultSeasonType"`
	Status               statusRecord    `json:"status"`
	Genres               []genreRecord   `json:"genres"`
	RemoteIDs            []remoteID      `json:"remoteIds"`
	Seasons              []seasonRecord  `json:"seasons"`
	OverviewTranslations []string        `json:"overviewTranslations"`
	Artworks             []artworkRecord `json:"artworks"`
}

// artworkRecord is one image entry in the series.artworks array.
// Type is the artwork-type ID per TVDB's artwork-types catalog;
// type 2 is the series poster. Score is provider-assigned ranking
// used to pick the "best" within a language pool.
type artworkRecord struct {
	ID        int     `json:"id"`
	Image     string  `json:"image"`
	Thumbnail string  `json:"thumbnail"`
	Language  string  `json:"language"`
	Type      int     `json:"type"`
	Score     float64 `json:"score"`
}

const artworkTypePoster = 2

func (c *client) seriesExtended(ctx context.Context, id string) (seriesExtendedRecord, error) {
	values := url.Values{}
	values.Set("meta", "translations")
	// short=true was previously set to keep the response small, but it
	// strips artworks. Drop it so the poster selection logic has data
	// to work with. Larger payload (~1.5x) is the trade-off.

	var out seriesExtendedResponse
	if err := c.get(ctx, "/series/"+url.PathEscape(id)+"/extended", values, &out); err != nil {
		return seriesExtendedRecord{}, err
	}
	return out.Data, nil
}

func (p *Provider) normalizeSeries(record seriesExtendedRecord, episodes []episodeRecord, preferredByID map[int]string) provider.Series {
	seasons := normalizeSeasons(record.Seasons, episodes, preferredByID)
	series := provider.Series{
		SeriesSummary: p.normalizeSeriesSummary(seriesSummaryInput{
			ref:              providerIntRef(record.ID),
			canonicalTitle:   record.Name,
			originalLanguage: record.OriginalLanguage,
			originalCountry:  record.OriginalCountry,
			firstAired:       record.FirstAired,
			status:           normalizeStatus(record.Status.Name),
			year:             yearFromDate(record.FirstAired),
			genres:           normalizeGenres(record.Genres),
			titles:           seriesTitleCandidates(record),
		}),
		LastAired: normalizeDate(record.LastAired),
		Seasons:   seasons,
		Poster:    p.selectPoster(record.Artworks),
	}
	return series
}

// selectPoster picks one poster from the artwork list using the
// caller's preferred-language order. Falls back to default-language
// (empty / explicit "") posters, then to any poster, then to none.
// Within each tier the highest score wins.
func (p *Provider) selectPoster(artworks []artworkRecord) provider.Artwork {
	posters := make([]artworkRecord, 0, len(artworks))
	for _, art := range artworks {
		if art.Type != artworkTypePoster {
			continue
		}
		if art.Image == "" {
			continue
		}
		posters = append(posters, art)
	}
	if len(posters) == 0 {
		return provider.Artwork{}
	}

	best := func(filter func(artworkRecord) bool) (artworkRecord, bool) {
		var pick artworkRecord
		found := false
		for _, art := range posters {
			if !filter(art) {
				continue
			}
			if !found || art.Score > pick.Score {
				pick = art
				found = true
			}
		}
		return pick, found
	}

	for _, lang := range p.preferredLanguages {
		normalized := normalizeLanguage(lang)
		if normalized == "" {
			continue
		}
		if pick, ok := best(func(a artworkRecord) bool { return normalizeLanguage(a.Language) == normalized }); ok {
			return artworkToProvider(pick)
		}
	}
	if pick, ok := best(func(a artworkRecord) bool { return a.Language == "" }); ok {
		return artworkToProvider(pick)
	}
	if pick, ok := best(func(artworkRecord) bool { return true }); ok {
		return artworkToProvider(pick)
	}
	return provider.Artwork{}
}

func artworkToProvider(a artworkRecord) provider.Artwork {
	return provider.Artwork{
		URL:          a.Image,
		ThumbnailURL: a.Thumbnail,
		Language:     normalizeLanguage(a.Language),
	}
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
