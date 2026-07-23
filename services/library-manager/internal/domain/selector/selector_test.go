package selector

import (
	"reflect"
	"testing"
)

func TestParseTerm(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want Term
	}{
		{name: "text", raw: "本好きの下剋上", want: Term("本好きの下剋上")},
		{name: "metadata ref", raw: "tvdb:370070", want: Term("tvdb:370070")},
		{name: "dirname is text", raw: "dir:foo", want: Term("dir:foo")},
		{name: "uppercase prefix", raw: "DIR:foo", want: Term("DIR:foo")},
		{name: "trim text", raw: "  X-Men  ", want: Term("X-Men")},
		{name: "spaces in value", raw: "tvdb:foo bar", want: Term("tvdb:foo bar")},
		{name: "empty", raw: "", want: Term("")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ParseTerm(test.raw); got != test.want {
				t.Fatalf("ParseTerm(%q) = %#v, want %#v", test.raw, got, test.want)
			}
		})
	}
}

func TestParseSelectorSkipsEmptyTerms(t *testing.T) {
	got := ParseSelector([]string{"a", "", "tvdb:1"})
	want := Selector{Terms: []Term{Term("a"), Term("tvdb:1")}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseSelector = %#v, want %#v", got, want)
	}
}

func TestTermString(t *testing.T) {
	tests := []struct {
		name string
		term Term
		want string
	}{
		{name: "text", term: Term("Bookworm"), want: "Bookworm"},
		{name: "prefixed", term: Term("tvdb:370070"), want: "tvdb:370070"},
		{name: "empty", term: Term(""), want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.term.String(); got != test.want {
				t.Fatalf("String = %q, want %q", got, test.want)
			}
		})
	}
}

func TestParseSelectorTreatsDirAsText(t *testing.T) {
	selector := ParseSelector([]string{"dir:Honzuki", "tvdb:370070"})
	if len(selector.Terms) != 2 {
		t.Fatalf("terms = %d", len(selector.Terms))
	}
	if selector.Terms[0].String() != "dir:Honzuki" {
		t.Fatalf("unexpected dir term: %#v", selector.Terms[0])
	}
	if selector.Terms[1].String() != "tvdb:370070" {
		t.Fatalf("unexpected tvdb term: %#v", selector.Terms[1])
	}
}
