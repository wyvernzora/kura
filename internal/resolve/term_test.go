package resolve

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
		{name: "text", raw: "本好きの下剋上", want: Term{Value: "本好きの下剋上"}},
		{name: "provider", raw: "tvdb:370070", want: Term{Prefix: "tvdb", Value: "370070"}},
		{name: "dirname", raw: "dir:foo", want: Term{Prefix: "dir", Value: "foo"}},
		{name: "uppercase prefix", raw: "DIR:foo", want: Term{Value: "DIR:foo"}},
		{name: "trim text", raw: "  X-Men  ", want: Term{Value: "X-Men"}},
		{name: "spaces in value", raw: "tvdb:foo bar", want: Term{Prefix: "tvdb", Value: "foo bar"}},
		{name: "empty", raw: "", want: Term{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ParseTerm(test.raw); got != test.want {
				t.Fatalf("ParseTerm(%q) = %#v, want %#v", test.raw, got, test.want)
			}
		})
	}
}

func TestParseQuery(t *testing.T) {
	got := ParseQuery([]string{"a", "", "tvdb:1"})
	want := Query{Terms: []Term{{Value: "a"}, {Prefix: "tvdb", Value: "1"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseQuery = %#v, want %#v", got, want)
	}
}
