package domain

import "testing"

func TestParseRemoteSeriesRef(t *testing.T) {
	ref, err := ParseRemoteSeriesRef(" TVDB:370070 ")
	if err != nil {
		t.Fatalf("ParseRemoteSeriesRef: %v", err)
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

func TestParseRemoteSeriesRefRejectsInvalidRefs(t *testing.T) {
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
			if _, err := ParseRemoteSeriesRef(value); err == nil {
				t.Fatal("ParseRemoteSeriesRef returned nil error, want rejection")
			}
		})
	}
}
