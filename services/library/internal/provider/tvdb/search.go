package tvdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/wyvernzora/kura/internal/provider"
	"github.com/wyvernzora/kura/internal/textnorm"
)

type searchResponse struct {
	Data []searchRecord `json:"data"`
}

type searchRecord struct {
	ID              tvdbString         `json:"id"`
	ObjectID        string             `json:"objectID"`
	TVDBID          tvdbString         `json:"tvdb_id"`
	Name            string             `json:"name"`
	NameTranslated  []string           `json:"name_translated"`
	Aliases         []string           `json:"aliases"`
	Translations    searchTranslations `json:"translations"`
	Genres          searchGenres       `json:"genres"`
	Type            string             `json:"type"`
	Status          string             `json:"status"`
	Score           float64            `json:"score"`
	Year            tvdbString         `json:"year"`
	FirstAired      string             `json:"first_air_time"`
	PrimaryLanguage string             `json:"primary_language"`
	Country         string             `json:"country"`
	ImageURL        string             `json:"image_url"`
	Thumbnail       string             `json:"thumbnail"`
	RemoteIDs       []remoteID         `json:"remote_ids"`
}

func (c *client) search(ctx context.Context, query string, opts provider.SearchOptions) ([]searchRecord, error) {
	values := url.Values{}
	values.Set("query", query)
	values.Set("type", "series")
	if opts.Limit > 0 {
		values.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Year > 0 {
		values.Set("year", strconv.Itoa(opts.Year))
	}
	var out searchResponse
	if err := c.get(ctx, "/search", values, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (p *Provider) normalizeSearchResult(record searchRecord) provider.SearchResult {
	id := firstNonEmpty(record.TVDBID.String(), trimObjectID(record.ObjectID), trimObjectID(record.ID.String()), record.ID.String())
	aliases := searchTitleAliases(record)

	return provider.SearchResult{
		SeriesSummary: p.normalizeSeriesSummary(seriesSummaryInput{
			ref:              providerRef(id),
			canonicalTitle:   record.Name,
			originalLanguage: record.PrimaryLanguage,
			originalCountry:  record.Country,
			firstAired:       record.FirstAired,
			status:           normalizeStatus(record.Status),
			year:             parseInt(record.Year.String()),
			genres:           record.Genres.Values,
			titles:           searchTitleCandidates(record),
			// Search carries a series poster URL directly, so candidate
			// lists get artwork without a per-result extended fetch.
			poster: provider.Artwork{URL: record.ImageURL, ThumbnailURL: record.Thumbnail},
		}),
		Score:       record.Score,
		MatchSource: "query",
		Aliases:     aliases,
	}
}

func searchTitleAliases(record searchRecord) []textnorm.NFCString {
	values := make([]string, 0, 1+len(record.NameTranslated)+len(record.Aliases)+len(record.Translations.Values))
	values = append(values, record.Name)
	values = append(values, record.NameTranslated...)
	values = append(values, record.Aliases...)
	for _, candidate := range searchTitleCandidates(record) {
		values = append(values, candidate.Value)
	}

	aliases := make([]textnorm.NFCString, 0, len(values))
	seen := map[string]struct{}{}
	for _, raw := range values {
		value := normalizeTitle(raw)
		if value.IsZero() {
			continue
		}
		key := value.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		aliases = append(aliases, value)
	}
	return aliases
}

func searchTitleCandidates(record searchRecord) []titleCandidate {
	titles := make([]titleCandidate, 0, len(record.Translations.Values))
	for _, translation := range record.Translations.Values {
		if translation.Value == "" {
			continue
		}
		titles = append(titles, titleCandidate{
			Language: translation.Language,
			Value:    translation.Value,
		})
	}
	return titles
}

func isSeriesRecord(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "" || value == "series"
}

type searchTranslations struct {
	Values []searchTranslation
}

type searchTranslation struct {
	Language string
	Value    string
}

type searchGenres struct {
	Values []string
}

func (g *searchGenres) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		g.Values = []string{}
		return nil
	}

	var stringsValue []string
	if err := json.Unmarshal(data, &stringsValue); err == nil {
		g.Values = normalizeGenreNames(stringsValue)
		return nil
	}

	var records []genreRecord
	if err := json.Unmarshal(data, &records); err == nil {
		g.Values = normalizeGenres(records)
		return nil
	}

	return fmt.Errorf("tvdb: expected search genres array, got %s", string(data))
}

func (t *searchTranslations) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		t.Values = nil
		return nil
	}

	var byLanguageString map[string]string
	if err := json.Unmarshal(data, &byLanguageString); err == nil {
		t.Values = make([]searchTranslation, 0, len(byLanguageString))
		for language, value := range byLanguageString {
			t.Values = append(t.Values, searchTranslation{
				Language: language,
				Value:    value,
			})
		}
		return nil
	}

	return fmt.Errorf("tvdb: expected search translations object, got %s", string(data))
}

func trimObjectID(id string) string {
	id = strings.TrimSpace(id)
	if after, ok := strings.CutPrefix(id, "series-"); ok {
		return after
	}
	return id
}
