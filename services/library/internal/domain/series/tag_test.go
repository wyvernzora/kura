package series

import (
	"reflect"
	"testing"
)

func TestValidateTag(t *testing.T) {
	tests := []struct {
		name    string
		tag     string
		wantErr bool
	}{
		{name: "kebab", tag: "maintenance-requested"},
		{name: "namespace", tag: "release:priority"},
		{name: "underscore", tag: "release_priority"},
		{name: "uppercase normalized before validation", tag: "Anime2026"},
		{name: "bang reserved", tag: "!priority", wantErr: true},
		{name: "leading punctuation", tag: "-priority", wantErr: true},
		{name: "whitespace", tag: "high priority", wantErr: true},
		{name: "too long", tag: "a1234567890123456789012345678901234567890123456789012345678901234", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTag(tt.tag)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateTag(%q) error = %v, wantErr %v", tt.tag, err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeTagsLowercasesDeduplicatesAndSorts(t *testing.T) {
	got, err := NormalizeTags([]string{"Priority", "maintenance-disabled", "PRIORITY"})
	if err != nil {
		t.Fatalf("NormalizeTags: %v", err)
	}
	want := []string{"maintenance-disabled", "priority"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeTags = %v, want %v", got, want)
	}
}
