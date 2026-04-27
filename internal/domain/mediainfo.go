package domain

import "encoding/json"

// MediaInfo stores parsed facts for one media file.
type MediaInfo struct {
	VideoCodec   string `json:"videoCodec,omitempty"`
	Resolution   string `json:"resolution,omitempty"`
	AudioCodec   string `json:"audioCodec,omitempty"`
	HasSubtitles bool   `json:"hasSubtitles"`
}

func (m MediaInfo) MarshalJSON() ([]byte, error) {
	type mediaInfo MediaInfo
	out := mediaInfo(m)
	return json.Marshal(out)
}
