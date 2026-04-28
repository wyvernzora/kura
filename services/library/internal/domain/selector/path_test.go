package selector_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/wyvernzora/kura/internal/domain/selector"
	"golang.org/x/text/unicode/norm"
)

func TestParse_HappyPath(t *testing.T) {
	cases := []struct {
		in     string
		scheme selector.Scheme
		rel    string
	}{
		{"inbox:foo.mkv", selector.Inbox, "foo.mkv"},
		{"inbox:[BDrip] Show Title/E01.mkv", selector.Inbox, "[BDrip] Show Title/E01.mkv"},
		{"series:Season 1/foo.mkv", selector.Series, "Season 1/foo.mkv"},
		{"inbox:weird:filename.mkv", selector.Inbox, "weird:filename.mkv"},
		{"inbox:foo//bar.mkv", selector.Inbox, "foo/bar.mkv"},
		{"inbox:./foo.mkv", selector.Inbox, "foo.mkv"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := selector.Parse(tc.in)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got.Scheme != tc.scheme {
				t.Errorf("scheme: got %q, want %q", got.Scheme, tc.scheme)
			}
			if got.Relative != tc.rel {
				t.Errorf("relative: got %q, want %q", got.Relative, tc.rel)
			}
		})
	}
}

func TestParse_Errors(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"missing_scheme", "foo.mkv"},
		{"empty_scheme", ":foo.mkv"},
		{"unknown_scheme", "tvdb:1"},
		{"unknown_scheme_archive", "archive:foo"},
		{"empty_relative", "inbox:"},
		{"leading_slash", "inbox:/foo"},
		{"backslash", "inbox:foo\\bar.mkv"},
		{"escaping_dotdot", "inbox:../foo"},
		{"escaping_deeper", "inbox:foo/../../bar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := selector.Parse(tc.in); err == nil {
				t.Fatalf("Parse(%q) expected error", tc.in)
			}
		})
	}
}

func TestParseInbox_RejectsOtherSchemes(t *testing.T) {
	if _, err := selector.ParseInbox("series:foo"); err == nil {
		t.Fatal("expected error for series: passed to ParseInbox")
	}
	if _, err := selector.ParseInbox("inbox:foo"); err != nil {
		t.Fatalf("inbox: should accept: %v", err)
	}
}

func TestParseSeries_RejectsOtherSchemes(t *testing.T) {
	if _, err := selector.ParseSeries("inbox:foo"); err == nil {
		t.Fatal("expected error for inbox: passed to ParseSeries")
	}
	if _, err := selector.ParseSeries("series:foo"); err != nil {
		t.Fatalf("series: should accept: %v", err)
	}
}

func TestPath_String(t *testing.T) {
	p, err := selector.Parse("series:Season 1/foo.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.String(); got != "series:Season 1/foo.mkv" {
		t.Errorf("String: got %q", got)
	}
	zero := selector.Path{}
	if zero.String() != "" {
		t.Errorf("zero String: got %q", zero.String())
	}
}

func TestPath_Resolve(t *testing.T) {
	p, _ := selector.Parse("inbox:[BDrip]/E01.mkv")
	got := p.Resolve("/mnt/inbox")
	want := filepath.Join("/mnt/inbox", "[BDrip]", "E01.mkv")
	if got != want {
		t.Errorf("Resolve: got %q, want %q", got, want)
	}
}

func TestPath_NFCNormalization(t *testing.T) {
	nfd := norm.NFD.String("café.mkv")
	p, err := selector.Parse("inbox:" + nfd)
	if err != nil {
		t.Fatal(err)
	}
	want := norm.NFC.String("café.mkv")
	if p.Relative != want {
		t.Errorf("relative: got %q, want %q", p.Relative, want)
	}
	if !norm.NFC.IsNormalString(p.Relative) {
		t.Errorf("not NFC: %q", p.Relative)
	}
}

func TestPath_JSONRoundTrip(t *testing.T) {
	p, err := selector.Parse("series:Season 2/E03.mkv")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"series:Season 2/E03.mkv"` {
		t.Errorf("Marshal: got %s", data)
	}
	var back selector.Path
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back != p {
		t.Errorf("round-trip: %+v vs %+v", p, back)
	}
}

func TestPath_JSONZero(t *testing.T) {
	zero := selector.Path{}
	data, err := json.Marshal(zero)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `""` {
		t.Errorf("zero Marshal: got %s", data)
	}
	var back selector.Path
	if err := json.Unmarshal([]byte(`""`), &back); err != nil {
		t.Fatal(err)
	}
	if !back.IsZero() {
		t.Errorf("empty unmarshal: got %+v", back)
	}
}

func TestCleanRelative(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  bool
	}{
		{"", "", false},
		{".", "", false},
		{"foo.mkv", "foo.mkv", false},
		{"foo//bar.mkv", "foo/bar.mkv", false},
		{"./foo.mkv", "foo.mkv", false},
		{"foo/../bar.mkv", "bar.mkv", false},
		{"/abs", "", true},
		{"foo\\bar", "", true},
		{"..", "", true},
		{"../foo", "", true},
	}
	for _, tc := range cases {
		got, err := selector.CleanRelative(tc.in)
		if tc.err {
			if err == nil {
				t.Errorf("CleanRelative(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("CleanRelative(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("CleanRelative(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
