package store

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed schema/trash_v1.json
var trashV1SchemaJSON []byte

type trashV1 struct {
	ID         string            `json:"id"`
	Season     int               `json:"season"`
	Number     int               `json:"number"`
	Media      mediaFileV1       `json:"media"`
	Companions []companionFileV1 `json:"companions"`
}

type trashDocumentV1 struct {
	SchemaVersion int       `json:"schemaVersion"`
	Entries       []trashV1 `json:"entries,omitempty"`
}

func decodeTrash(data []byte, path string) (Trash, error) {
	var header schemaHeader
	if err := json.Unmarshal(data, &header); err != nil {
		return Trash{}, fmt.Errorf("library: decode %s: %w", path, err)
	}
	switch header.SchemaVersion {
	case TrashSchemaVersion:
		return decodeTrashV1(data, path)
	default:
		return Trash{}, fmt.Errorf("library: unsupported trash schemaVersion %d", header.SchemaVersion)
	}
}

func encodeTrash(w io.Writer, trash Trash) error {
	if trash.SchemaVersion != TrashSchemaVersion {
		return fmt.Errorf("library: unsupported trash schemaVersion %d", trash.SchemaVersion)
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(trashDocumentToV1(trash))
}

func decodeTrashV1(data []byte, path string) (Trash, error) {
	if err := validateTrashV1JSON(data); err != nil {
		return Trash{}, fmt.Errorf("library: validate %s: %w", path, err)
	}
	var disk trashDocumentV1
	if err := json.Unmarshal(data, &disk); err != nil {
		return Trash{}, fmt.Errorf("library: decode %s: %w", path, err)
	}
	return trashDocumentFromV1(disk), nil
}

func trashDocumentToV1(trash Trash) trashDocumentV1 {
	return trashDocumentV1{
		SchemaVersion: TrashSchemaVersion,
		Entries:       trashEntriesToV1(trash.Entries),
	}
}

func trashDocumentFromV1(disk trashDocumentV1) Trash {
	return Trash{
		SchemaVersion: disk.SchemaVersion,
		Entries:       trashEntriesFromV1(disk.Entries),
	}
}

func trashEntriesToV1(trash []TrashedEpisode) []trashV1 {
	if len(trash) == 0 {
		return nil
	}
	out := make([]trashV1, 0, len(trash))
	for _, trashed := range trash {
		out = append(out, trashV1{
			ID:         trashed.ID,
			Season:     trashed.Season,
			Number:     trashed.Number,
			Media:      mediaFileToV1(trashed.Media),
			Companions: companionsToV1(trashed.Companions),
		})
	}
	return out
}

func trashEntriesFromV1(trash []trashV1) []TrashedEpisode {
	if len(trash) == 0 {
		return nil
	}
	out := make([]TrashedEpisode, 0, len(trash))
	for _, trashed := range trash {
		out = append(out, TrashedEpisode{
			ID:     trashed.ID,
			Season: trashed.Season,
			Number: trashed.Number,
			Episode: Episode{
				Number:     trashed.Number,
				Media:      mediaFileFromV1(trashed.Media),
				Companions: companionsFromV1(trashed.Companions),
			},
		})
	}
	return out
}

var (
	trashV1SchemaOnce sync.Once
	trashV1Schema     *jsonschema.Schema
	trashV1SchemaErr  error
)

func validateTrashV1Schema(trash trashDocumentV1) error {
	data, err := json.Marshal(trash)
	if err != nil {
		return err
	}
	return validateTrashV1JSON(data)
}

func validateTrashV1JSON(data []byte) error {
	schema, err := compiledTrashV1Schema()
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

func compiledTrashV1Schema() (*jsonschema.Schema, error) {
	trashV1SchemaOnce.Do(func() {
		var doc any
		decoder := json.NewDecoder(bytes.NewReader(trashV1SchemaJSON))
		decoder.UseNumber()
		if err := decoder.Decode(&doc); err != nil {
			trashV1SchemaErr = err
			return
		}
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource("trash_v1.json", doc); err != nil {
			trashV1SchemaErr = err
			return
		}
		trashV1Schema, trashV1SchemaErr = compiler.Compile("trash_v1.json")
	})
	return trashV1Schema, trashV1SchemaErr
}
