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
}
