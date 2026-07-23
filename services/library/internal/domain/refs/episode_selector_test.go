package refs_test

import (
	"errors"
	"testing"

	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
)

func TestParseEpisodeSelector_Forms(t *testing.T) {
	cases := []struct {
		in       string
		wantStr  string
		wantZero bool
		matchYes []refs.Episode
		matchNo  []refs.Episode
	}{
		{
			in:       "",
			wantStr:  "",
			wantZero: true,
			matchYes: []refs.Episode{
				mustEp(t, 1, 1),
				mustEp(t, 99, 99),
			},
		},
		{
			in:      "S01",
			wantStr: "S1",
			matchYes: []refs.Episode{
				mustEp(t, 1, 1),
				mustEp(t, 1, 99),
			},
			matchNo: []refs.Episode{
				mustEp(t, 0, 1),
				mustEp(t, 2, 1),
			},
		},
		{
			in:      "S0",
			wantStr: "S0",
			matchYes: []refs.Episode{
				mustEp(t, 0, 1),
			},
			matchNo: []refs.Episode{
				mustEp(t, 1, 1),
			},
		},
		{
			in:      "S01E03",
			wantStr: "S1E3",
			matchYes: []refs.Episode{
				mustEp(t, 1, 3),
			},
			matchNo: []refs.Episode{
				mustEp(t, 1, 2),
				mustEp(t, 1, 4),
				mustEp(t, 2, 3),
			},
		},
		{
			in:      "S01E03-12",
			wantStr: "S1E3-12",
			matchYes: []refs.Episode{
				mustEp(t, 1, 3),
				mustEp(t, 1, 7),
				mustEp(t, 1, 12),
			},
			matchNo: []refs.Episode{
				mustEp(t, 1, 2),
				mustEp(t, 1, 13),
				mustEp(t, 2, 7),
			},
		},
		{
			in:      "ALL",
			wantStr: "ALL",
			matchYes: []refs.Episode{
				mustEp(t, 0, 1),
				mustEp(t, 1, 1),
			},
		},
		{
			in:      "NONE",
			wantStr: "NONE",
			matchNo: []refs.Episode{
				mustEp(t, 0, 1),
				mustEp(t, 1, 1),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := refs.ParseEpisodeSelector(tc.in)
			if err != nil {
				t.Fatalf("ParseEpisodeSelector(%q): %v", tc.in, err)
			}
			if got.IsZero() != tc.wantZero {
				t.Errorf("IsZero = %v, want %v", got.IsZero(), tc.wantZero)
			}
			if got.String() != tc.wantStr {
				t.Errorf("String = %q, want %q", got.String(), tc.wantStr)
			}
			for _, ref := range tc.matchYes {
				if !got.Matches(ref) {
					t.Errorf("Matches(%s) = false, want true", ref)
				}
			}
			for _, ref := range tc.matchNo {
				if got.Matches(ref) {
					t.Errorf("Matches(%s) = true, want false", ref)
				}
			}
		})
	}
}

func TestParseEpisodeSelector_AiringSeason(t *testing.T) {
	got, err := refs.ParseEpisodeSelector("AIRING_SEASON")
	if err != nil {
		t.Fatalf("ParseEpisodeSelector: %v", err)
	}
	if !got.IsAiringSeason() || got.IsZero() {
		t.Fatalf("got %+v, want explicit AIRING_SEASON selector", got)
	}
	if got.String() != "AIRING_SEASON" {
		t.Fatalf("String = %q, want AIRING_SEASON", got.String())
	}
	defer func() {
		if recover() == nil {
			t.Fatal("Matches(AIRING_SEASON) did not panic")
		}
	}()
	got.Matches(mustEp(t, 1, 1))
}

func TestParseEpisodeSelector_Errors(t *testing.T) {
	cases := []string{
		"S",
		"S1E",
		"S1E0",        // episode 0 invalid
		"S1E5-3",      // start > end
		"S1E0-5",      // start 0
		"E1",          // missing season
		"s01e03",      // lowercase
		"none",        // keyword is exact-case
		"S01E03-12-1", // extra range part
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			_, err := refs.ParseEpisodeSelector(in)
			if err == nil {
				t.Fatalf("ParseEpisodeSelector(%q): nil err, want InvalidEpisodeSelectorError", in)
			}
			var inv *refs.InvalidEpisodeSelectorError
			if !errors.As(err, &inv) {
				t.Fatalf("err = %T, want *InvalidEpisodeSelectorError", err)
			}
		})
	}
}

func mustEp(t *testing.T, season, episode int) refs.Episode {
	t.Helper()
	ref, err := refs.NewEpisode(season, episode)
	if err != nil {
		t.Fatalf("NewEpisode(%d, %d): %v", season, episode, err)
	}
	return ref
}
