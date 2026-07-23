package config

import (
	"slices"
	"testing"
)

func TestParsePreferredLanguagesNormalizesAndDedupes(t *testing.T) {
	got, err := ParsePreferredLanguages(" ja, en-US ja ")
	if err != nil {
		t.Fatalf("ParsePreferredLanguages: %v", err)
	}
	if !slices.Equal(got.Tags(), []string{"ja", "en-US"}) {
		t.Fatalf("Tags = %#v, want [ja en-US]", got.Tags())
	}
}

func TestParsePreferredLanguagesEmpty(t *testing.T) {
	got, err := ParsePreferredLanguages("")
	if err != nil {
		t.Fatalf("ParsePreferredLanguages: %v", err)
	}
	if !got.IsEmpty() {
		t.Fatal("IsEmpty = false, want true")
	}
}

func TestParsePreferredLanguagesRejectsInvalidTag(t *testing.T) {
	if _, err := ParsePreferredLanguages("not_a_language_tag"); err == nil {
		t.Fatal("ParsePreferredLanguages returned nil error, want invalid tag rejection")
	}
}
