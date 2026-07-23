package cli_test

import (
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/library/internal/cli"
)

func TestParseDuration(t *testing.T) {
	cases := []struct {
		input string
		want  time.Duration
	}{
		{"", 0},
		{" ", 0},
		{"30s", 30 * time.Second},
		{"5m", 5 * time.Minute},
		{"24h", 24 * time.Hour},
		{"30d", 30 * 24 * time.Hour},
		{"2w", 14 * 24 * time.Hour},
		{"0d", 0},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := cli.ParseDuration(tc.input)
			if err != nil {
				t.Fatalf("ParseDuration(%q): %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("ParseDuration(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseDurationRejects(t *testing.T) {
	cases := []string{
		"x",
		"1",
		"1y",
		"-5d",
		"5.5d",
		"abc",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if _, err := cli.ParseDuration(in); err == nil {
				t.Fatalf("ParseDuration(%q): nil err, want error", in)
			}
		})
	}
}
