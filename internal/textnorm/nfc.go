package textnorm

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

type NFCString string

func NFC(value string) NFCString {
	return NFCString(norm.NFC.String(strings.TrimSpace(value)))
}

func (s NFCString) String() string {
	return string(s)
}

func (s NFCString) IsZero() bool {
	return s == ""
}
