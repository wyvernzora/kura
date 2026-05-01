package refs

import (
	"encoding/json"
	"testing"
)

func TestMetadata(t *testing.T) {
	ref, err := ParseMetadata("tvdb:370070")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Provider() != "tvdb" || ref.ID() != "370070" || ref.String() != "tvdb:370070" {
		t.Fatalf("unexpected metadata ref: %#v", ref)
	}
}

func TestSeries(t *testing.T) {
	ref, err := ParseSeries("Honzuki")
	if err != nil {
		t.Fatal(err)
	}
	if ref.String() != "Honzuki" {
		t.Fatalf("unexpected series ref %q", ref)
	}
	if _, err := ParseSeries("Season 1/Bookworm"); err == nil {
		t.Fatal("expected separator series name error")
	}
	if _, err := ParseSeries(".kura"); err == nil {
		t.Fatal("expected reserved series name error")
	}
	normalized, err := ParseSeries("Cafe\u0301")
	if err != nil {
		t.Fatal(err)
	}
	if normalized.String() != "Café" {
		t.Fatalf("normalized series ref = %q, want Café", normalized)
	}
}

func TestEpisode(t *testing.T) {
	ref, err := NewEpisode(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if ref.String() != "S01E0001" {
		t.Fatalf("storage ref = %q", ref.String())
	}
	if ref.Marker() != "S01E01" {
		t.Fatalf("marker = %q", ref.Marker())
	}
	parsed, err := ParseEpisode("S01E0001")
	if err != nil {
		t.Fatal(err)
	}
	if parsed != ref {
		t.Fatalf("parsed %#v, want %#v", parsed, ref)
	}
	data, err := json.Marshal(ref)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"S01E0001"` {
		t.Fatalf("json = %s, want episode ref string", data)
	}
	var decoded Episode
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != ref {
		t.Fatalf("decoded %#v, want %#v", decoded, ref)
	}
}

func TestParseSelectorTreatsDirAsText(t *testing.T) {
	selector := ParseSelector([]string{"dir:Honzuki", "tvdb:370070"})
	if len(selector.Terms) != 2 {
		t.Fatalf("terms = %d", len(selector.Terms))
	}
	if selector.Terms[0].Prefix != "" || selector.Terms[0].Value.String() != "dir:Honzuki" {
		t.Fatalf("unexpected dir term: %#v", selector.Terms[0])
	}
	if selector.Terms[1].Prefix != "tvdb" || selector.Terms[1].Value.String() != "370070" {
		t.Fatalf("unexpected tvdb term: %#v", selector.Terms[1])
	}
}
