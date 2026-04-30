package wire

import (
	"encoding/json"
)

func Encode(series Series) ([]byte, error) {
	series.SchemaVersion = CurrentSchemaVersion
	if series.Episodes == nil {
		series.Episodes = map[string]Episode{}
	}
	for key, episode := range series.Episodes {
		if episode.Active != nil && episode.Active.Companions == nil {
			episode.Active.Companions = []CompanionRecord{}
		}
		if episode.Staged != nil && episode.Staged.Companions == nil {
			episode.Staged.Companions = []CompanionRecord{}
		}
		series.Episodes[key] = episode
	}
	data, err := json.MarshalIndent(series, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if err := validateSeries(data); err != nil {
		return nil, err
	}
	return data, nil
}
