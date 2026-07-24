package main

import (
	"strings"
	"testing"
)

func TestParseUmask(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{name: "zero", raw: "0000", want: 0o000},
		{name: "owner default", raw: "0022", want: 0o022},
		{name: "group writable", raw: "0002", want: 0o002},
		{name: "three digits", raw: "077", want: 0o077},
		{name: "max", raw: "0777", want: 0o777},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseUmask(tc.raw)
			if err != nil {
				t.Fatalf("parseUmask(%q): %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("parseUmask(%q) = %#o, want %#o", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseUmaskRejectsInvalidValues(t *testing.T) {
	for _, raw := range []string{"", "888", "0o022", "1000", "abc"} {
		_, err := parseUmask(raw)
		if err == nil {
			t.Fatalf("parseUmask(%q) returned nil error", raw)
		}
		if !strings.Contains(err.Error(), "server.umask") {
			t.Fatalf("parseUmask(%q) error = %v, want mention of server.umask", raw, err)
		}
	}
}
