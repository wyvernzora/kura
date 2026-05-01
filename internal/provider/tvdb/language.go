package tvdb

import (
	"strings"

	"golang.org/x/text/language"
)

func normalizeLanguages(languages []string) []string {
	out := make([]string, 0, len(languages))
	seen := map[string]bool{}
	for _, language := range languages {
		normalized := normalizeLanguage(language)
		if normalized == "" || seen[normalized] {
			continue
		}
		out = append(out, normalized)
		seen[normalized] = true
	}
	return out
}

func normalizeLanguage(tagValue string) string {
	tagValue = strings.TrimSpace(tagValue)
	if tagValue == "" {
		return ""
	}

	switch strings.ToLower(strings.ReplaceAll(tagValue, "_", "-")) {
	case "zhtw", "zh-tw", "zh-hant":
		return "zh-TW"
	}

	tag, err := language.Parse(tagValue)
	if err != nil {
		return strings.ToLower(strings.ReplaceAll(tagValue, "_", "-"))
	}
	return tag.String()
}

func normalizeCountry(regionValue string) string {
	regionValue = strings.TrimSpace(regionValue)
	if regionValue == "" {
		return ""
	}
	region, err := language.ParseRegion(regionValue)
	if err != nil {
		return strings.ToUpper(strings.ReplaceAll(regionValue, "_", "-"))
	}
	return region.String()
}
