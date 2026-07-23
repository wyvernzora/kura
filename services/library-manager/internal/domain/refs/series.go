package refs

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/wyvernzora/kura/services/library-manager/internal/textnorm"
)

// Series identifies a tracked series by directory name.
type Series struct {
	value textnorm.NFCString
}

func ParseSeries(value string) (Series, error) {
	normalized := textnorm.NFC(value)
	if normalized.IsZero() {
		return Series{}, errors.New("series name is required")
	}
	value = normalized.String()
	if value == "." || value == ".." || value == ".kura" {
		return Series{}, fmt.Errorf("invalid series name %q", value)
	}
	if strings.ContainsFunc(value, func(r rune) bool {
		return r == '/' || r == '\\' || unicode.IsControl(r)
	}) {
		return Series{}, fmt.Errorf("invalid series name %q", value)
	}
	return Series{value: normalized}, nil
}

func (ref Series) String() string {
	return ref.value.String()
}

func (ref Series) IsZero() bool {
	return ref.value.IsZero()
}

func (ref Series) MarshalJSON() ([]byte, error) {
	return json.Marshal(ref.String())
}

func (ref *Series) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, err := ParseSeries(value)
	if err != nil {
		return err
	}
	*ref = parsed
	return nil
}
