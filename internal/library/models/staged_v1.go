package models

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed staged_v1.schema.json
var stagedV1SchemaJSON []byte

type stagedV1 struct {
	Season     int               `json:"season"`
	Number     int               `json:"number"`
	Media      mediaFileV1       `json:"media"`
	Companions []companionFileV1 `json:"companions"`
}

type stagedDocumentV1 struct {
	SchemaVersion int        `json:"schemaVersion"`
	Entries       []stagedV1 `json:"entries,omitempty"`
}

func decodeStaged(data []byte, path string) (Staged, error) {
	var header schemaHeader
	if err := json.Unmarshal(data, &header); err != nil {
		return Staged{}, fmt.Errorf("library: decode %s: %w", path, err)
	}
	switch header.SchemaVersion {
	case StagedSchemaVersion:
		return decodeStagedV1(data, path)
	default:
		return Staged{}, fmt.Errorf("library: unsupported staged schemaVersion %d", header.SchemaVersion)
	}
}

func encodeStaged(w io.Writer, staged Staged) error {
	if staged.SchemaVersion != StagedSchemaVersion {
		return fmt.Errorf("library: unsupported staged schemaVersion %d", staged.SchemaVersion)
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(stagedDocumentToV1(staged))
}

func decodeStagedV1(data []byte, path string) (Staged, error) {
	if err := validateStagedV1JSON(data); err != nil {
		return Staged{}, fmt.Errorf("library: validate %s: %w", path, err)
	}
	var disk stagedDocumentV1
	if err := json.Unmarshal(data, &disk); err != nil {
		return Staged{}, fmt.Errorf("library: decode %s: %w", path, err)
	}
	return stagedDocumentFromV1(disk), nil
}

func stagedDocumentToV1(staged Staged) stagedDocumentV1 {
	return stagedDocumentV1{
		SchemaVersion: StagedSchemaVersion,
		Entries:       stagedEntriesToV1(staged.Entries),
	}
}

func stagedDocumentFromV1(disk stagedDocumentV1) Staged {
	return Staged{
		SchemaVersion: disk.SchemaVersion,
		Entries:       stagedEntriesFromV1(disk.Entries),
	}
}

func stagedEntriesToV1(entries []StagedEpisode) []stagedV1 {
	if len(entries) == 0 {
		return nil
	}
	out := make([]stagedV1, 0, len(entries))
	for _, staged := range entries {
		out = append(out, stagedV1{
			Season:     staged.Season,
			Number:     staged.Number,
			Media:      mediaFileToV1(staged.Media),
			Companions: companionsToV1(staged.Companions),
		})
	}
	return out
}

func stagedEntriesFromV1(entries []stagedV1) []StagedEpisode {
	if len(entries) == 0 {
		return nil
	}
	out := make([]StagedEpisode, 0, len(entries))
	for _, staged := range entries {
		out = append(out, StagedEpisode{
			Season: staged.Season,
			Number: staged.Number,
			Episode: Episode{
				Media:      mediaFileFromV1(staged.Media),
				Companions: companionsFromV1(staged.Companions),
			},
		})
	}
	return out
}

var (
	stagedV1SchemaOnce sync.Once
	stagedV1Schema     *jsonschema.Schema
	stagedV1SchemaErr  error
)

func validateStagedV1Schema(staged stagedDocumentV1) error {
	data, err := json.Marshal(staged)
	if err != nil {
		return err
	}
	return validateStagedV1JSON(data)
}

func validateStagedV1JSON(data []byte) error {
	schema, err := compiledStagedV1Schema()
	if err != nil {
		return err
	}
	var doc any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		return err
	}
	return schema.Validate(doc)
}

func compiledStagedV1Schema() (*jsonschema.Schema, error) {
	stagedV1SchemaOnce.Do(func() {
		var doc any
		decoder := json.NewDecoder(bytes.NewReader(stagedV1SchemaJSON))
		decoder.UseNumber()
		if err := decoder.Decode(&doc); err != nil {
			stagedV1SchemaErr = err
			return
		}
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource("staged_v1.schema.json", doc); err != nil {
			stagedV1SchemaErr = err
			return
		}
		stagedV1Schema, stagedV1SchemaErr = compiler.Compile("staged_v1.schema.json")
	})
	return stagedV1Schema, stagedV1SchemaErr
}
