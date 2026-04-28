package media

import "strings"

type Codec string

func ParseCodec(codec string) Codec {
	return Codec(strings.TrimSpace(codec))
}

func (c Codec) String() string {
	return string(c)
}

func (c Codec) Known() bool {
	return strings.TrimSpace(string(c)) != ""
}
