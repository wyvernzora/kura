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

type seriesSummaryInput struct {
	ref              string
	canonicalTitle   string
	originalLanguage string
	originalCountry  string
	firstAired       string
	status           metadata.SeriesStatus
	year             int
	genres           []string
	linkedRefs       []string
	titles           []titleCandidate
}

func (p *Provider) normalizeSeriesSummary(input seriesSummaryInput) metadata.SeriesSummary {
	canonicalTitle := normalizeTitle(input.canonicalTitle)
	return metadata.SeriesSummary{
		ProviderRef:      input.ref,
		ProviderRefs:     providerRefs(input.ref, input.linkedRefs),
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
