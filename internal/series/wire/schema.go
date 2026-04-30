package wire

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
	seriesV1Once sync.Once
	seriesV1     *jsonschema.Schema
	seriesV1Err  error
)

func validateSeries(data []byte) error {
	seriesV1Once.Do(func() {
		var schemaDoc any
		decoder := json.NewDecoder(bytes.NewReader(mustReadSchema()))
		decoder.UseNumber()
		if seriesV1Err = decoder.Decode(&schemaDoc); seriesV1Err != nil {
			return
		}
		compiler := jsonschema.NewCompiler()
		seriesV1Err = compiler.AddResource("series_v1.json", schemaDoc)
		if seriesV1Err != nil {
			return
		}
		seriesV1, seriesV1Err = compiler.Compile("series_v1.json")
	})
	if seriesV1Err != nil {
		return seriesV1Err
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if err := seriesV1.Validate(value); err != nil {
		return fmt.Errorf("validate series: %w", err)
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
