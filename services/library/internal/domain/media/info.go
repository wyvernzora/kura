package media

import "context"

type Inspector interface {
	Inspect(context.Context, string) (Info, error)
}

type Info struct {
	VideoCodec   string `json:"videoCodec,omitempty"`
	Resolution   string `json:"resolution,omitempty"`
	AudioCodec   string `json:"audioCodec,omitempty"`
	HasSubtitles bool   `json:"hasSubtitles"`
	// Title is the container's General.Title field — the embedded
	// "name" the encoder set when the file was authored (mkvtoolnix
	// --title, ffmpeg -metadata title, etc.). Often empty, often
	// filled with junk; useful only as a heuristic fallback when
	// filename source inference fails.
	Title string `json:"title,omitempty"`
}
