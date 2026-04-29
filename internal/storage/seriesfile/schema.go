package seriesfile

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed schema/series_v2.json
var schemaFS embed.FS

var (
	v2SchemaOnce sync.Once
	v2Schema     *jsonschema.Schema
	v2SchemaErr  error
)

func loadV2Schema() (*jsonschema.Schema, error) {
	v2SchemaOnce.Do(func() {
		raw, err := schemaFS.ReadFile("schema/series_v2.json")
		if err != nil {
			v2SchemaErr = err
			return
		}
		var schemaDoc any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if v2SchemaErr = decoder.Decode(&schemaDoc); v2SchemaErr != nil {
			return
		}
		compiler := jsonschema.NewCompiler()
		if v2SchemaErr = compiler.AddResource("series_v2.json", schemaDoc); v2SchemaErr != nil {
			return
		}
		v2Schema, v2SchemaErr = compiler.Compile("series_v2.json")
	})
	return v2Schema, v2SchemaErr
}

// validateSeries validates data against the v2 schema. The version
// parameter is retained for symmetry with prior multi-version support;
// today only v2 is accepted, so any other value errors.
func validateSeries(version int, data []byte) error {
	if version != currentSchemaVersion {
		return fmt.Errorf("seriesfile: unsupported schemaVersion %d", version)
	}
	schema, err := loadV2Schema()
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
