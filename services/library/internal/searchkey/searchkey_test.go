package searchkey

import (
	"strings"
	"testing"

	"github.com/wyvernzora/kura/internal/provider"
)

func TestComputeFlattensSingleAlias(t *testing.T) {
	got := Compute(Inputs{
		Canonical: "葬送のフリーレン",
		Preferred: "葬送のフリーレン",
		Aliases: []provider.TitleEntry{
			{Language: "", Value: "Frieren"},
		},
	})
	if !containsLine(got, "frieren") {
		t.Fatalf("Compute = %q, want flat line 'frieren'", got)
	}
}

func TestComputeJoinsMultiWordAliasIntoSingleLine(t *testing.T) {
	got := Compute(Inputs{
		Canonical: "Frieren",
		Aliases: []provider.TitleEntry{
			{Language: "", Value: "Sousou no Frieren"},
		},
	})
	// Whitespace and punctuation collapse — one alias = one line.
	// "frieren" alone would dedupe against canonical; the joined
	// form is a different line and survives.
	if !containsLine(got, "sousounofrieren") {
		t.Fatalf("Compute = %q, want flat 'sousounofrieren'", got)
	}
}

func TestComputeStripsPunctuationAndDiacritics(t *testing.T) {
	got := Compute(Inputs{
		Canonical: "Tonari",
		Aliases: []provider.TitleEntry{
			{Language: "", Value: "Tonari no Kaibutsu-kun"},
			{Language: "", Value: "Imōto"},
		},
	})
	if !containsLine(got, "tonarinokaibutsukun") {
		t.Fatalf("Compute = %q, want hyphen + spaces collapsed", got)
	}
	// NFKD strips the macron; "ō" → "o" (not "ou").
	if !containsLine(got, "imoto") {
		t.Fatalf("Compute = %q, want diacritics dropped (imōto → imoto)", got)
	}
}

func TestComputeDropsAliasMatchingDisplay(t *testing.T) {
	got := Compute(Inputs{
		Canonical: "Frieren",
		Preferred: "Frieren",
		Aliases: []provider.TitleEntry{
			{Language: "", Value: "Frieren"}, // flat == canonical → drop
			{Language: "", Value: "Sousou"},  // flat != canonical → keep
		},
	})
	if containsLine(got, "frieren") {
		t.Fatalf("Compute = %q, expected alias matching canonical to drop", got)
	}
	if !containsLine(got, "sousou") {
		t.Fatalf("Compute = %q, want 'sousou'", got)
	}
}

func TestComputeDropsCJKOnlyAliases(t *testing.T) {
	got := Compute(Inputs{
		Canonical: "葬送のフリーレン",
		Aliases: []provider.TitleEntry{
			{Language: "ja", Value: "葬送のフリーレン"}, // CJK-only, dropped
			{Language: "", Value: "Frieren"},    // Latin, kept
		},
	})
	if !containsLine(got, "frieren") {
		t.Fatalf("Compute = %q, want 'frieren'", got)
	}
	if strings.Contains(got, "葬送") {
		t.Fatalf("Compute = %q, expected CJK-only alias dropped", got)
	}
}

func TestComputeFiltersTranslationsByPreferredLangs(t *testing.T) {
	in := Inputs{
		Canonical: "葬送のフリーレン",
		TranslatedTitles: []TranslatedTitle{
			{Language: "en", Value: "Frieren Beyond Journeys End"},
			{Language: "fr", Value: "Frieren Au-dela du Voyage"},
		},
		PreferredLangs: []string{"ja", "en"},
	}
	got := Compute(in)
	if !containsLine(got, "frierenbeyondjourneysend") {
		t.Fatalf("Compute = %q, want flat en translation line", got)
	}
	if strings.Contains(got, "voyage") {
		t.Fatalf("Compute = %q, expected fr translation dropped", got)
	}
}

func TestComputeUserAliasesIncluded(t *testing.T) {
	got := Compute(Inputs{
		Canonical:   "俺の妹がこんなに可愛いわけがない",
		UserAliases: []string{"oreimo", "imouto"},
	})
	if !containsLine(got, "oreimo") || !containsLine(got, "imouto") {
		t.Fatalf("Compute = %q, want user aliases", got)
	}
}

func TestComputeIdempotent(t *testing.T) {
	in := Inputs{
		Canonical: "Frieren",
		Aliases: []provider.TitleEntry{
			{Value: "Sousou no Frieren"},
		},
		UserAliases:    []string{"frieren-jp"},
		PreferredLangs: []string{"ja"},
	}
	first := Compute(in)
	second := Compute(in)
	if first != second {
		t.Fatalf("Compute not deterministic: %q vs %q", first, second)
	}
}

func TestComputeStableSort(t *testing.T) {
	got := Compute(Inputs{
		Aliases: []provider.TitleEntry{
			{Value: "zebra"}, {Value: "apple"}, {Value: "mango"},
		},
	})
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %v, want 3", lines)
	}
	for i := 1; i < len(lines); i++ {
		if lines[i-1] >= lines[i] {
			t.Fatalf("lines not sorted: %v", lines)
		}
	}
}

func TestComputeEmptyWhenNoCandidates(t *testing.T) {
	got := Compute(Inputs{
		Canonical: "葬送のフリーレン",
	})
	if got != "" {
		t.Fatalf("Compute = %q, want empty", got)
	}
}

func containsLine(blob, line string) bool {
	for _, l := range strings.Split(blob, "\n") {
		if l == line {
			return true
		}
	}
	return false
}
