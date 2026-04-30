package series

import "testing"

func TestSeriesDirRejectsEscapingRelPath(t *testing.T) {
	seriesDir, err := ParseSeriesDir(t.TempDir())
	if err != nil {
		t.Fatalf("ParseSeriesDir: %v", err)
	}
	if _, err := seriesDir.JoinRel("../episode.mkv"); err == nil {
		t.Fatal("JoinRel returned nil error, want escaping path rejection")
	}
	if _, err := seriesDir.JoinRel(".kura/series.json"); err == nil {
		t.Fatal("JoinRel returned nil error, want .kura path rejection")
	}
}
