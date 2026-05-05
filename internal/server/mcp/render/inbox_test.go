package render

import (
	"strings"
	"testing"

	"github.com/wyvernzora/kura/internal/response"
)

func TestInboxList_BasicFormatting(t *testing.T) {
	in := response.InboxList{
		Path: "[BDrip] Hoshi.../",
		Entries: []response.InboxEntry{
			{Path: "[BDrip] Hoshi.../E01.mkv", Kind: "file", Size: 1234567890, MTime: "2026-05-01T03:14:00Z"},
			{Path: "[BDrip] Hoshi.../Subs", Kind: "dir", MTime: "2026-05-01T03:14:00Z"},
		},
	}
	out := InboxList(in)

	if !strings.Contains(out, "F   ") {
		t.Errorf("missing F kind glyph: %q", out)
	}
	if !strings.Contains(out, "D ") {
		t.Errorf("missing D kind glyph: %q", out)
	}
	if !strings.Contains(out, "1.15GB") {
		t.Errorf("missing human size 1.15GB: %q", out)
	}
	if !strings.Contains(out, "2026-05-01T03:14Z") {
		t.Errorf("missing minute-truncated mtime: %q", out)
	}
	if !strings.Contains(out, "Subs/") {
		t.Errorf("dir should have trailing slash: %q", out)
	}
}

func TestInboxList_SymlinkTargetSurfacedWithArrow(t *testing.T) {
	in := response.InboxList{
		Entries: []response.InboxEntry{
			{Path: "link", Kind: "symlink", SymlinkTarget: "/elsewhere", MTime: "2026-04-22T18:02:00Z"},
		},
	}
	out := InboxList(in)
	if !strings.Contains(out, "L ") {
		t.Errorf("missing L kind glyph: %q", out)
	}
	if !strings.Contains(out, "link -> /elsewhere") {
		t.Errorf("missing symlink arrow target: %q", out)
	}
	if !strings.Contains(out, "      -") {
		t.Errorf("symlink should have '-' size column: %q", out)
	}
}

func TestInboxList_TruncationFooterAndHint(t *testing.T) {
	in := response.InboxList{
		Entries: []response.InboxEntry{
			{Path: "a.mkv", Kind: "file", Size: 1024, MTime: "2026-05-01T03:14:00Z"},
		},
		Truncated:   true,
		ElidedCount: 47,
		Hint:        []string{`pass path="<subdir>" to list a single subtree`, "drop recursive to list only the immediate children"},
	}
	out := InboxList(in)
	if !strings.Contains(out, "... [1 entries shown above; 47 entries elided]") {
		t.Errorf("missing truncation footer: %q", out)
	}
	if !strings.Contains(out, "# Narrow scope to retry:") {
		t.Errorf("missing hint header: %q", out)
	}
	if !strings.Contains(out, `#   pass path="<subdir>"`) {
		t.Errorf("missing hint line: %q", out)
	}
}

func TestInboxList_Empty(t *testing.T) {
	out := InboxList(response.InboxList{})
	if out != "(empty)\n" {
		t.Errorf("empty: got %q", out)
	}
}

func TestTruncateToMinuteUTC(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"2026-05-01T03:14:00Z", "2026-05-01T03:14Z"},
		{"", ""},
		{"weird", "weird"},
	}
	for _, tc := range cases {
		if got := truncateToMinuteUTC(tc.in); got != tc.want {
			t.Errorf("truncateToMinuteUTC(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
