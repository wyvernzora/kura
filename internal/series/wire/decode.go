package wire

import (
	"encoding/json"
	"fmt"
)

type schemaHeader struct {
	SchemaVersion int `json:"schemaVersion"`
}

func Decode(data []byte) (SeriesV1, error) {
	var header schemaHeader
	if err := json.Unmarshal(data, &header); err != nil {
		return SeriesV1{}, fmt.Errorf("decode series: %w", err)
	}
	if header.SchemaVersion != CurrentSchemaVersion {
		return SeriesV1{}, fmt.Errorf("unsupported series schemaVersion %d", header.SchemaVersion)
	}
	if err := validateSeries(data); err != nil {
		return SeriesV1{}, err
	}
	var series SeriesV1
	if err := json.Unmarshal(data, &series); err != nil {
		return SeriesV1{}, fmt.Errorf("decode series: %w", err)
	}
	if series.Episodes == nil {
		series.Episodes = map[string]EpisodeV1{}
	}
	return series, nil
}
