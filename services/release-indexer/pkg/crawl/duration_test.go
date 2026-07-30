package crawl

import (
	"strings"
	"testing"
	"time"
)

func TestParseLookback(t *testing.T) {
	tests := []struct {
		raw  string
		want time.Duration
	}{
		{raw: "", want: 0},
		{raw: "0", want: 0},
		{raw: "36h", want: 36 * time.Hour},
		{raw: "30d", want: 30 * 24 * time.Hour},
		{raw: "2w", want: 14 * 24 * time.Hour},
		{raw: "2w3d12h", want: (17*24 + 12) * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := ParseLookback("dmhy", tt.raw)
			if err != nil {
				t.Fatalf("ParseLookback(%q) error = %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("ParseLookback(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseLookbackRejectsMalformedAndOverflow(t *testing.T) {
	for _, raw := range []string{"-1h", "1x", "999999999999999999999999w", "15251w6d"} {
		t.Run(raw, func(t *testing.T) {
			_, err := ParseLookback("nyaa", raw)
			if err == nil {
				t.Fatalf("ParseLookback(%q) accepted", raw)
			}
			if !strings.Contains(err.Error(), "malformed lookback") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
