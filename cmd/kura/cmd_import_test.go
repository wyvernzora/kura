package main

import (
	"slices"
	"testing"
)

func TestImportCmdResolveTerms(t *testing.T) {
	tests := []struct {
		name string
		cmd  importCmd
		want []string
	}{
		{
			name: "dirname only",
			cmd:  importCmd{Dirname: "whatever"},
			want: []string{"whatever"},
		},
		{
			name: "dirname plus text terms",
			cmd:  importCmd{Dirname: "whatever", Terms: []string{"foo", "bar"}},
			want: []string{"whatever", "foo", "bar"},
		},
		{
			name: "empty extra terms",
			cmd:  importCmd{Dirname: "whatever", Terms: []string{"", "  "}},
			want: []string{"whatever", "", "  "},
		},
		{
			name: "metadata ref term",
			cmd:  importCmd{Dirname: "whatever", Terms: []string{"tvdb:370070"}},
			want: []string{"tvdb:370070"},
		},
		{
			name: "metadata ref term with empty term",
			cmd:  importCmd{Dirname: "whatever", Terms: []string{"", "tvdb:370070"}},
			want: []string{"tvdb:370070"},
		},
		{
			name: "metadata ref mixed with text terms",
			cmd:  importCmd{Dirname: "whatever", Terms: []string{"foo", "tvdb:370070"}},
			want: []string{"foo", "tvdb:370070"},
		},
		{
			name: "multiple metadata ref terms",
			cmd:  importCmd{Dirname: "whatever", Terms: []string{"tvdb:370070", "tvdb:999999"}},
			want: []string{"tvdb:370070", "tvdb:999999"},
		},
		{
			name: "dir term treated as text",
			cmd:  importCmd{Dirname: "whatever", Terms: []string{"dir:tracked"}},
			want: []string{"whatever", "dir:tracked"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.cmd.resolveTerms()
			if err != nil {
				t.Fatalf("resolveTerms error = %v", err)
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("resolveTerms = %#v, want %#v", got, test.want)
			}
		})
	}
}
