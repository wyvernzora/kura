package tvdb

import (
	"strconv"
	"strings"

	"github.com/wyvernzora/kura/internal/metadata"
	"golang.org/x/text/unicode/norm"
)

type titleCandidate struct {
	Language string
	Value    string
}

func providerRef(id string) string {
	if id == "" {
		return ""
	}
	return providerKey + ":" + id
}

func providerIntRef(id int) string {
	if id <= 0 {
		return ""
	}
	return providerRef(strconv.Itoa(id))
}

func (p *Provider) normalizeSeriesSummary(ref, canonicalTitle, originalLanguage, originalCountry, firstAired string, status metadata.SeriesStatus, year int, genres []string, linkedRefs []string, titles []titleCandidate) metadata.SeriesSummary {
	canonicalTitle = normalizeTitle(canonicalTitle)
	return metadata.SeriesSummary{
		ProviderRef:      ref,
		ProviderRefs:     providerRefs(ref, linkedRefs),
		PreferredTitle:   p.selectTitle(canonicalTitle, originalLanguage, titles),
		CanonicalTitle:   canonicalTitle,
		Type:             metadata.MediaTypeSeries,
		Status:           status,
		Year:             year,
		OriginalLanguage: normalizeLanguage(originalLanguage),
		OriginalCountry:  normalizeCountry(originalCountry),
		FirstAired:       normalizeDate(firstAired),
		Genres:           genres,
	}
}

func providerRefs(primary string, linked []string) []string {
	refs := make([]string, 0, 1+len(linked))
	seen := map[string]bool{}
	if primary != "" {
		refs = append(refs, primary)
		seen[primary] = true
	}
	for _, ref := range linked {
		if ref == "" || seen[ref] {
			continue
		}
		refs = append(refs, ref)
		seen[ref] = true
	}
	return refs
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
