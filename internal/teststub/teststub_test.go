//go:build e2e_stub

package teststub

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wyvernzora/kura/internal/provider"
)

func TestNewDefaultProvider_GetSeries(t *testing.T) {
	p := NewDefaultProvider()
	s, err := p.GetSeries(context.Background(), "1001", "")
	if err != nil {
		t.Fatalf("GetSeries 1001: %v", err)
	}
	if s.PreferredTitle.String() != "Stub Show" {
		t.Errorf("PreferredTitle: got %q want Stub Show", s.PreferredTitle.String())
	}
	if len(s.Seasons) != 1 || len(s.Seasons[0].Episodes) != 3 {
		t.Errorf("seasons/episodes shape: %+v", s.Seasons)
	}
}

func TestNewDefaultProvider_GetSeries_Unknown(t *testing.T) {
	p := NewDefaultProvider()
	_, err := p.GetSeries(context.Background(), "9999", "")
	if !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("err: got %v want ErrNotFound", err)
	}
}

func TestLoadProvider_EmptyPath(t *testing.T) {
	p, err := LoadProvider("")
	if err != nil {
		t.Fatalf("LoadProvider empty path: %v", err)
	}
	if _, err := p.GetSeries(context.Background(), "1001", ""); err != nil {
		t.Errorf("default fixture missing 1001: %v", err)
	}
}

func TestLoadProvider_FromFile(t *testing.T) {
	dir := t.TempDir()
	doc := map[string]any{
		"series": map[string]any{
			"42": map[string]any{
				"metadataRef":    "stub:42",
				"preferredTitle": "Custom",
				"canonicalTitle": "Custom",
				"status":         "continuing",
				"seasons":        []any{},
			},
		},
	}
	buf, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "fixture.json")
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadProvider(path)
	if err != nil {
		t.Fatalf("LoadProvider: %v", err)
	}
	s, err := p.GetSeries(context.Background(), "42", "")
	if err != nil {
		t.Fatalf("GetSeries 42: %v", err)
	}
	if s.PreferredTitle.String() != "Custom" {
		t.Errorf("title: got %q want Custom", s.PreferredTitle.String())
	}
}

func TestNewDefaultInspector_ReturnsCannedFacts(t *testing.T) {
	i := NewDefaultInspector()
	info, err := i.Inspect(context.Background(), "/some/path")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if info.Resolution != "1920x1080" {
		t.Errorf("resolution: got %q", info.Resolution)
	}
	if info.VideoCodec != "H.264" {
		t.Errorf("codec: got %q", info.VideoCodec)
	}
}
