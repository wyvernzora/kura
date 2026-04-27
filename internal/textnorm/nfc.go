package textnorm

import (
	"encoding/json"
	"strings"

	"golang.org/x/text/unicode/norm"
)

type NFCString struct {
	value string
}

func NFC(value string) NFCString {
	return NFCString{value: norm.NFC.String(strings.TrimSpace(value))}
}

func (s NFCString) String() string {
	return s.value
}

func (s NFCString) IsZero() bool {
	return s.value == ""
}

func (s NFCString) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.value)
}

func (s *NFCString) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*s = NFC(value)
	return nil
}

func (s NFCString) MarshalText() ([]byte, error) {
	return []byte(s.value), nil
}

func (s *NFCString) UnmarshalText(data []byte) error {
	*s = NFC(string(data))
	return nil
}
