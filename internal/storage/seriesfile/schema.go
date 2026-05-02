package seriesfile

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed schema/series_v1.json
var schemaFS embed.FS

var (
	seriesV1SchemaOnce sync.Once
	seriesV1Schema     *jsonschema.Schema
	seriesV1SchemaErr  error
)

func validateSeries(data []byte) error {
	seriesV1SchemaOnce.Do(func() {
		var schemaDoc any
		decoder := json.NewDecoder(bytes.NewReader(mustReadSchema()))
		decoder.UseNumber()
		if seriesV1SchemaErr = decoder.Decode(&schemaDoc); seriesV1SchemaErr != nil {
			return
		}
		compiler := jsonschema.NewCompiler()
		seriesV1SchemaErr = compiler.AddResource("series_v1.json", schemaDoc)
		if seriesV1SchemaErr != nil {
			return
		}
		seriesV1Schema, seriesV1SchemaErr = compiler.Compile("series_v1.json")
	})
	if seriesV1SchemaErr != nil {
		return seriesV1SchemaErr
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if err := seriesV1Schema.Validate(value); err != nil {
		return fmt.Errorf("seriesfile: validate: %w", err)
	}
	return nil
}

func mustReadSchema() []byte {
	data, err := schemaFS.ReadFile("schema/series_v1.json")
	if err != nil {
		panic(err)
	}
	return data
}
