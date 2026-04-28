package refs_test

import (
	"errors"
	"testing"

	"github.com/wyvernzora/kura/internal/domain/refs"
)

func TestParseEpisodeSelector_Forms(t *testing.T) {
	cases := []struct {
		in       string
		want     refs.EpisodeSelector
		wantStr  string
		matchYes []refs.Episode
		matchNo  []refs.Episode
	}{
		{
			in:      "",
			want:    refs.EpisodeSelector{},
			wantStr: "",
			matchYes: []refs.Episode{
				mustEp(t, 1, 1),
				mustEp(t, 99, 99),
			},
		},
		{
			in:      "S01",
			want:    refs.EpisodeSelector{Active: true, Season: 1},
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
			want:    refs.EpisodeSelector{Active: true, Season: 0},
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
			want:    refs.EpisodeSelector{Active: true, Season: 1, HasRange: true, From: 3, To: 3},
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
			want:    refs.EpisodeSelector{Active: true, Season: 1, HasRange: true, From: 3, To: 12},
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
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := refs.ParseEpisodeSelector(tc.in)
			if err != nil {
				t.Fatalf("ParseEpisodeSelector(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
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

func TestParseEpisodeSelector_Errors(t *testing.T) {
	cases := []string{
		"S",
		"S1E",
		"S1E0",        // episode 0 invalid
		"S1E5-3",      // start > end
		"S1E0-5",      // start 0
		"E1",          // missing season
		"s01e03",      // lowercase
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
