// Package schema centralizes JSON-schema plumbing for store documents.
package schema

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed series_v1.json
var seriesV1Raw []byte

//go:embed staged_v1.json
var stagedV1Raw []byte

//go:embed trash_v1.json
var trashV1Raw []byte

var (
	once       sync.Once
	seriesV1   *jsonschema.Schema
	stagedV1   *jsonschema.Schema
	trashV1    *jsonschema.Schema
	compileErr error
)

// SeriesV1 returns the compiled series_v1 schema.
func SeriesV1() (*jsonschema.Schema, error) { return load(&seriesV1) }

// StagedV1 returns the compiled staged_v1 schema.
func StagedV1() (*jsonschema.Schema, error) { return load(&stagedV1) }

// TrashV1 returns the compiled trash_v1 schema.
func TrashV1() (*jsonschema.Schema, error) { return load(&trashV1) }

func load(target **jsonschema.Schema) (*jsonschema.Schema, error) {
	once.Do(func() {
		if seriesV1, compileErr = compile("series_v1.json", seriesV1Raw); compileErr != nil {
			return
		}
		if stagedV1, compileErr = compile("staged_v1.json", stagedV1Raw); compileErr != nil {
			return
		}
		trashV1, compileErr = compile("trash_v1.json", trashV1Raw)
	})
	if compileErr != nil {
		return nil, compileErr
	}
	return *target, nil
}

func compile(name string, raw []byte) (*jsonschema.Schema, error) {
	var doc any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("schema: decode %s: %w", name, err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(name, doc); err != nil {
		return nil, fmt.Errorf("schema: add %s: %w", name, err)
	}
	return compiler.Compile(name)
}

// ValidateBytes validates raw JSON data against the supplied schema.
//
// Numbers decode via json.Decoder with UseNumber so the JSON-schema validator
// applies type constraints to integers correctly.
func ValidateBytes(s *jsonschema.Schema, data []byte) error {
	var doc any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		return err
	}
	return s.Validate(doc)
}

// ValidateValue marshals v and validates the result against the supplied schema.
func ValidateValue(s *jsonschema.Schema, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return ValidateBytes(s, data)
}
