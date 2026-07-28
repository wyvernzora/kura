// Package seriesmeta reads the library manager facts needed to archive a
// series.
package seriesmeta

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"unicode/utf8"
)

const schemaVersion = 3

// Metadata contains the archive-relevant coordination facts from series.json.
type Metadata struct {
	Generation      int
	MetadataRef     string
	HasStagedIntent bool
	HasActiveClaim  bool
}

type schemaHeader struct {
	SchemaVersion int `json:"schemaVersion"`
}

// partialSeriesV3 mirrors seriesfile's tags and json.Unmarshal field matching.
// Making this decoder stricter would let the two services disagree on one file.
type partialSeriesV3 struct {
	Generation   int                       `json:"generation,omitempty"`
	MetadataRef  string                    `json:"metadataRef"`
	Episodes     map[string]partialEpisode `json:"episodes"`
	StagedTrash  []json.RawMessage         `json:"stagedTrash"`
	StagedExtras []json.RawMessage         `json:"stagedExtras"`
	InProgress   *struct{}                 `json:"in_progress,omitempty"`
}

type partialEpisode struct {
	Staged *struct{} `json:"staged,omitempty"`
}

// Read reads the partial series.json v3 metadata at path.
func Read(path string) (Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, fmt.Errorf("seriesmeta: read: %w", err)
	}
	return decode(data)
}

func decode(data []byte) (Metadata, error) {
	if !utf8.Valid(data) {
		return Metadata{}, errors.New("seriesmeta: series.json must be valid UTF-8")
	}

	var header schemaHeader
	if err := json.Unmarshal(data, &header); err != nil {
		return Metadata{}, fmt.Errorf("seriesmeta: decode: %w", err)
	}
	if header.SchemaVersion != schemaVersion {
		return Metadata{}, fmt.Errorf(
			"seriesmeta: unsupported schemaVersion %d",
			header.SchemaVersion,
		)
	}

	var series partialSeriesV3
	if err := json.Unmarshal(data, &series); err != nil {
		return Metadata{}, fmt.Errorf("seriesmeta: decode: %w", err)
	}
	if series.Generation < 1 {
		series.Generation = 1
	}

	metadata := Metadata{
		Generation:      series.Generation,
		MetadataRef:     series.MetadataRef,
		HasStagedIntent: len(series.StagedTrash) > 0 || len(series.StagedExtras) > 0,
		HasActiveClaim:  series.InProgress != nil,
	}
	for _, episode := range series.Episodes {
		if episode.Staged != nil {
			metadata.HasStagedIntent = true
			break
		}
	}
	return metadata, nil
}
