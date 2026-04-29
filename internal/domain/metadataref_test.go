package domain

import "testing"

func TestParseMetadataRef(t *testing.T) {
	ref, err := ParseMetadataRef(" TVDB:370070 ")
	if err != nil {
		t.Fatalf("ParseMetadataRef: %v", err)
	}
	if ref.Source() != "tvdb" {
		t.Fatalf("Source = %q, want tvdb", ref.Source())
	}
	if ref.ID() != "370070" {
		t.Fatalf("ID = %q, want 370070", ref.ID())
	}
	if ref.String() != "tvdb:370070" {
		t.Fatalf("String = %q, want tvdb:370070", ref.String())
	}
}

func TestParseMetadataRefRejectsInvalidRefs(t *testing.T) {
	for _, value := range []string{
		"",
		"tvdb",
		":370070",
		"tvdb:",
		"tv db:370070",
		"tvdb:37 0070",
		"tvdb:370:070",
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := ParseMetadataRef(value); err == nil {
				t.Fatal("ParseMetadataRef returned nil error, want rejection")
			}
		})
	}
}
