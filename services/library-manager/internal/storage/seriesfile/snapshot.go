package seriesfile

import (
	"encoding/json"
	"errors"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/series"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/paths"
)

// Encode returns the compact series.json v3 wire object used inside the
// library index snapshot. Active and staged-trash paths are series-relative,
// matching the on-disk series.json shape.
func Encode(libRoot string, m *series.Series) ([]byte, error) {
	if m == nil {
		return nil, errors.New("seriesfile: Encode called with nil Series")
	}
	if m.Ref.IsZero() {
		return nil, errors.New("seriesfile: Encode called with zero Ref")
	}
	seriesDir := paths.SeriesDir(libRoot, m.Ref)
	wire, err := toWire(m)
	if err != nil {
		return nil, err
	}
	if err := relativizeActiveWire(&wire, seriesDir); err != nil {
		return nil, err
	}
	normalizeWire(&wire)
	data, err := json.Marshal(wire)
	if err != nil {
		return nil, err
	}
	if err := validateSeries(currentSchemaVersion, data); err != nil {
		return nil, err
	}
	return data, nil
}

// Decode parses a compact snapshot series object. It sets Ref from the index
// line and leaves Hash empty because snapshot bytes are not the CAS source.
func Decode(libRoot string, ref refs.Series, data []byte) (*series.Series, error) {
	if ref.IsZero() {
		return nil, errors.New("seriesfile: Decode called with zero Ref")
	}
	wire, err := decodeWire(data, false)
	if err != nil {
		return nil, err
	}
	model, err := fromWire(wire)
	if err != nil {
		return nil, err
	}
	model.Ref = ref
	model.Hash = ""
	absolutizeActive(model, paths.SeriesDir(libRoot, ref))
	return model, nil
}
