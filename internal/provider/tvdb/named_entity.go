package tvdb

import (
	"strconv"
	"strings"
	"unicode"

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
	poster           provider.Artwork
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
		Poster:           input.poster,
	}
}

// selectTitle picks the best title for the operator's preferred-
// language list out of the per-language translation set the source
// returned. Walks `preferredLanguages` in order; first match wins.
// Final fallback returns the canonical title — the provider's
// default — when no preferred-lang translation exists.
//
// `originalLanguage` lets us synthesize a translation entry under
// that language slot when the canonical text's script is *consistent*
// with the language. We deliberately gate this on script-detection
// because TVDB editors sometimes use a romaji or English title as the
// canonical for a JP-origin show, and stamping that under the `ja`
// slot used to lie ("preferredTitle: Tomodachi no Imōto..." for a
// series whose translations contain real JP text but no `jpn`
// nameTranslation). Latin canonical for a CJK `originalLanguage`
// skips the backfill; the prefs walk falls through and the final
// canonical-fallback surfaces the same string with honest semantics.
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
			if canonicalMatchesLanguageScript(canonicalTitle.String(), canonicalLanguage) {
				byLanguage[canonicalLanguage] = canonicalTitle
			}
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

// canonicalMatchesLanguageScript reports whether canonical text is
// "compatible" with the claimed language by script. CJK languages
// (ja, zh, ko) require the canonical to contain at least one CJK
// letter (hiragana / katakana / Han / hangul); a purely-Latin
// canonical for a CJK originalLanguage means the upstream record's
// canonical is romaji or English, not a real CJK translation.
//
// Other languages return true unconditionally — the caller's existing
// canonical-fallback path is the safety net for the rare case where
// a CJK-only canonical claims an English originalLanguage.
func canonicalMatchesLanguageScript(canonical, lang string) bool {
	if canonical == "" || lang == "" {
		return false
	}
	base := strings.ToLower(strings.SplitN(lang, "-", 2)[0])
	switch base {
	case "ja", "zh", "ko":
	default:
		return true
	}
	for _, r := range canonical {
		if !unicode.IsLetter(r) {
			continue
		}
		if unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) ||
			unicode.Is(unicode.Han, r) ||
			unicode.Is(unicode.Hangul, r) {
			return true
		}
	}
	return false
}

func normalizeTitle(title string) textnorm.NFCString {
	return textnorm.NFC(title)
}
