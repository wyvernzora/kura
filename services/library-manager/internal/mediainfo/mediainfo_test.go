package mediainfo

import (
	"testing"
)

func TestParseJSONExtractsKuraMediaInfo(t *testing.T) {
	input := []byte(`{
		"media": {
			"@ref": "episode.mkv",
			"track": [
				{
					"@type": "General",
					"UniqueID": "123456789",
					"OverallBitRate": "12400000"
				},
				{
					"@type": "Video",
					"Format": "HEVC",
					"CodecID": "V_MPEGH/ISO/HEVC",
					"Width": "1920",
					"Height": "1080",
					"BitRate": "12000000"
				},
				{
					"@type": "Audio",
					"Format": "FLAC",
					"Channels": "2",
					"Language": "ja"
				},
				{
					"@type": "Text",
					"Format": "ASS",
					"Language": "en",
					"Title": "Commie"
				}
			]
		}
	}`)

	got, err := ParseJSON(input)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	if got.VideoCodec != "HEVC" {
		t.Fatalf("VideoCodec = %q, want HEVC", got.VideoCodec)
	}
	if got.Resolution != "1920x1080" {
		t.Fatalf("Resolution = %q, want 1920x1080", got.Resolution)
	}
	if got.AudioCodec != "FLAC" {
		t.Fatalf("AudioCodec = %q, want FLAC", got.AudioCodec)
	}
	if !got.HasSubtitles {
		t.Fatal("HasSubtitles = false, want true")
	}
}

func TestParseJSONHandlesNumericFields(t *testing.T) {
	input := []byte(`{
		"media": {
			"track": [
				{
					"@type": "Video",
					"Format": "AVC",
					"Width": 1280,
					"Height": 720,
					"BitRate": 3500000
				},
				{
					"@type": "Audio",
					"Format": "AAC",
					"Channels": 5.1,
					"Language": null
				}
			]
		}
	}`)

	got, err := ParseJSON(input)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	if got.VideoCodec != "H.264" {
		t.Fatalf("VideoCodec = %q, want H.264", got.VideoCodec)
	}
	if got.Resolution != "1280x720" {
		t.Fatalf("Resolution = %q, want 1280x720", got.Resolution)
	}
	if got.AudioCodec != "AAC" {
		t.Fatalf("AudioCodec = %q, want AAC", got.AudioCodec)
	}
}

func TestParseJSONReportsNoSubtitles(t *testing.T) {
	input := []byte(`{
		"media": {
			"track": [
				{
					"@type": "Video",
					"Format": "HEVC",
					"Width": 1920,
					"Height": 1080
				}
			]
		}
	}`)

	got, err := ParseJSON(input)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	if got.HasSubtitles {
		t.Fatal("HasSubtitles = true, want false")
	}
}

func TestParseJSONRejectsInvalidJSON(t *testing.T) {
	if _, err := ParseJSON([]byte(`{`)); err == nil {
		t.Fatal("ParseJSON returned nil error, want decode error")
	}
}
