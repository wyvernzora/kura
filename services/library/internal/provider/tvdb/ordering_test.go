package tvdb

import "testing"

func TestParseOrderingAcceptsCanonical(t *testing.T) {
	for _, v := range Orderings() {
		got, err := ParseOrdering(v)
		if err != nil {
			t.Errorf("ParseOrdering(%q) err = %v", v, err)
		}
		if got != v {
			t.Errorf("ParseOrdering(%q) = %q", v, got)
		}
	}
}

func TestParseOrderingEmptyIsValid(t *testing.T) {
	got, err := ParseOrdering("")
	if err != nil {
		t.Fatalf("ParseOrdering(\"\") err = %v", err)
	}
	if got != "" {
		t.Fatalf("ParseOrdering(\"\") = %q, want empty", got)
	}
}

func TestParseOrderingRejectsUnknown(t *testing.T) {
	for _, v := range []string{"DVD", "Default", "bogus", " dvd"} {
		if _, err := ParseOrdering(v); err == nil {
			t.Errorf("ParseOrdering(%q) err = nil, want error", v)
		}
	}
}
