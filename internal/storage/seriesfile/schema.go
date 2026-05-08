package seriesfile

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed schema/series_v3.json
var schemaFS embed.FS

var (
	v3SchemaOnce sync.Once
	v3Schema     *jsonschema.Schema
	v3SchemaErr  error
)

// loadSchema returns the embedded schema for the requested version.
// Only v3 is supported; pre-v3 files were soft-migrated through an
// earlier release and are no longer accepted.
func loadSchema(version int) (*jsonschema.Schema, error) {
	if version != currentSchemaVersion {
		return nil, fmt.Errorf("seriesfile: no embedded schema for v%d", version)
	}
	v3SchemaOnce.Do(func() {
		v3Schema, v3SchemaErr = compileSchema("schema/series_v3.json")
	})
	return v3Schema, v3SchemaErr
}

func compileSchema(path string) (*jsonschema.Schema, error) {
	raw, err := schemaFS.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(path, doc); err != nil {
		return nil, err
	}
	return compiler.Compile(path)
}

// validateSeries validates data against the embedded v3 schema.
// `version` is retained for symmetry with prior multi-version support;
// any value other than currentSchemaVersion errors.
func validateSeries(version int, data []byte) error {
	schema, err := loadSchema(version)
	if err != nil {
		return err
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if err := schema.Validate(value); err != nil {
		return fmt.Errorf("seriesfile: validate: %w", err)
	}
	return nil
}
