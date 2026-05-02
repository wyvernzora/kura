package tvdb

import (
	"strconv"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/provider"
	"github.com/wyvernzora/kura/internal/textnorm"
)

type titleCandidate struct {
	Language string
	Value    string
}

func providerRef(id string) refs.Metadata {
	if id == "" {
		return ""
	}
	return refs.Metadata(providerKey + ":" + id)
}

func providerIntRef(id int) refs.Metadata {
	if id <= 0 {
		return ""
	}
	return providerRef(strconv.Itoa(id))
}

type seriesSummaryInput struct {
	ref              refs.Metadata
	canonicalTitle   string
	originalLanguage string
	originalCountry  string
	firstAired       string
	status           provider.SeriesStatus
	year             int
	genres           []string
	titles           []titleCandidate
}

func (p *Provider) normalizeSeriesSummary(input seriesSummaryInput) provider.SeriesSummary {
	canonicalTitle := normalizeTitle(input.canonicalTitle)
	return provider.SeriesSummary{
		MetadataRef:      input.ref,
		PreferredTitle:   p.selectTitle(canonicalTitle, input.originalLanguage, input.titles),
		CanonicalTitle:   canonicalTitle,
		Type:             provider.MediaTypeSeries,
		Status:           input.status,
		Year:             input.year,
		OriginalLanguage: normalizeLanguage(input.originalLanguage),
		OriginalCountry:  normalizeCountry(input.originalCountry),
		FirstAired:       normalizeDate(input.firstAired),
		Genres:           input.genres,
	}
}

func (p *Provider) selectTitle(canonicalTitle textnorm.NFCString, originalLanguage string, titles []titleCandidate) textnorm.NFCString {
	byLanguage := make(map[string]textnorm.NFCString, len(titles))
	for _, title := range titles {
		value := normalizeTitle(title.Value)
		if value.IsZero() {
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
	if !canonicalTitle.IsZero() && canonicalLanguage != "" {
		if _, ok := byLanguage[canonicalLanguage]; !ok {
			byLanguage[canonicalLanguage] = canonicalTitle
		}
	}

	for _, language := range p.preferredLanguages {
		value := byLanguage[language]
		if !value.IsZero() {
			return value
		}
	}

	return normalizeTitle(canonicalTitle.String())
}

func normalizeTitle(title string) textnorm.NFCString {
	return textnorm.NFC(title)
}
