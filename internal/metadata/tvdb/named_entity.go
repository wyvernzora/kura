package tvdb

import (
	"strconv"
	"strings"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/metadata"
	"golang.org/x/text/unicode/norm"
)

type titleCandidate struct {
	Language string
	Value    string
}

func providerRef(id string) domain.MetadataRef {
	if id == "" {
		return ""
	}
	return domain.MetadataRef(providerKey + ":" + id)
}

func providerIntRef(id int) domain.MetadataRef {
	if id <= 0 {
		return ""
	}
	return providerRef(strconv.Itoa(id))
}

type seriesSummaryInput struct {
	ref              domain.MetadataRef
	canonicalTitle   string
	originalLanguage string
	originalCountry  string
	firstAired       string
	status           metadata.SeriesStatus
	year             int
	genres           []string
	titles           []titleCandidate
}

func (p *Provider) normalizeSeriesSummary(input seriesSummaryInput) metadata.SeriesSummary {
	canonicalTitle := normalizeTitle(input.canonicalTitle)
	return metadata.SeriesSummary{
		MetadataRef:      input.ref,
		PreferredTitle:   p.selectTitle(canonicalTitle, input.originalLanguage, input.titles),
		CanonicalTitle:   canonicalTitle,
		Type:             metadata.MediaTypeSeries,
		Status:           input.status,
		Year:             input.year,
		OriginalLanguage: normalizeLanguage(input.originalLanguage),
		OriginalCountry:  normalizeCountry(input.originalCountry),
		FirstAired:       normalizeDate(input.firstAired),
		Genres:           input.genres,
	}
}

func (p *Provider) selectTitle(canonicalTitle, originalLanguage string, titles []titleCandidate) string {
	byLanguage := make(map[string]string, len(titles))
	for _, title := range titles {
		value := normalizeTitle(title.Value)
		if value == "" {
			continue
		}
		language := normalizeLanguage(title.Language)
		if language == "" {
			continue
		}
		if _, ok := byLanguage[language]; ok {
			continue
		}
		byLanguage[language] = value
	}

	canonicalLanguage := normalizeLanguage(originalLanguage)
	if canonicalTitle != "" && canonicalLanguage != "" {
		if _, ok := byLanguage[canonicalLanguage]; !ok {
			byLanguage[canonicalLanguage] = canonicalTitle
		}
	}

	for _, language := range p.preferredLanguages {
		value := byLanguage[language]
		if value != "" {
			return value
		}
	}

	return normalizeTitle(canonicalTitle)
}

func normalizeTitle(title string) string {
	return norm.NFC.String(strings.TrimSpace(title))
}
