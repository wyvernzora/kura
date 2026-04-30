package wire

import (
	"encoding/json"
	"fmt"
)

type schemaHeader struct {
	SchemaVersion int `json:"schemaVersion"`
}

func Decode(data []byte) (Series, error) {
	var header schemaHeader
	if err := json.Unmarshal(data, &header); err != nil {
		return Series{}, fmt.Errorf("decode series: %w", err)
	}
	if header.SchemaVersion != CurrentSchemaVersion {
		return Series{}, fmt.Errorf("unsupported series schemaVersion %d", header.SchemaVersion)
	}
	if err := validateSeries(data); err != nil {
		return Series{}, err
	}
	var series Series
	if err := json.Unmarshal(data, &series); err != nil {
		return Series{}, fmt.Errorf("decode series: %w", err)
	}
	if series.Episodes == nil {
		series.Episodes = map[string]Episode{}
	}
	return series, nil
}
