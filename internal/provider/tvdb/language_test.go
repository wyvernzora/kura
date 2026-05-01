package tvdb

import "testing"

func TestNormalizeLanguageReturnsBCP47(t *testing.T) {
	tests := map[string]string{
		"eng":   "en",
		"jpn":   "ja",
		"por":   "pt",
		"pt_BR": "pt-BR",
		"zho":   "zh",
		"zhtw":  "zh-TW",
	}

	for in, want := range tests {
		if got := normalizeLanguage(in); got != want {
			t.Fatalf("normalizeLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeCountryReturnsISO3166Alpha2(t *testing.T) {
	tests := map[string]string{
		"jpn": "JP",
		"JPN": "JP",
		"us":  "US",
	}

	for in, want := range tests {
		if got := normalizeCountry(in); got != want {
			t.Fatalf("normalizeCountry(%q) = %q, want %q", in, got, want)
		}
	}
}
