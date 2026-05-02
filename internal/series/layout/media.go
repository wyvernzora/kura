package layout

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/media"
	"github.com/wyvernzora/kura/internal/textnorm"
)

type FileTitle struct {
	value textnorm.NFCString
}

func ParseFileTitle(title string) (FileTitle, error) {
	clean := CleanFileTitle(title)
	if clean.IsZero() {
		return FileTitle{}, errors.New("library: filesystem title is required")
	}
	if invalidFileTitle(clean.String()) {
		return FileTitle{}, fmt.Errorf("library: invalid filesystem title %q", title)
	}
	return clean, nil
}

func CleanFileTitle(title string) FileTitle {
	return FileTitle{value: textnorm.NFC(title)}
}

func (t FileTitle) String() string {
	return t.value.String()
}

func (t FileTitle) IsZero() bool {
	return t.value.IsZero()
}

func (t FileTitle) EqualName(name string) bool {
	return t == CleanFileTitle(name)
}

func invalidFileTitle(title string) bool {
	return strings.ContainsFunc(title, func(r rune) bool {
		return r == '/' || r == '\\' || r == 0 || unicode.IsControl(r)
	})
}

type MediaFilename string

type MediaFilenameFacts struct {
	Source     media.Source
	Resolution media.Resolution
}

func BuildMediaFilename(title FileTitle, episode refs.Episode, facts MediaFilenameFacts, extension string) MediaFilename {
	source := facts.Source.Display()
	resolution := facts.Resolution.Display()
	if resolution == "" {
		resolution = "UnknownResolution"
	}
	return MediaFilename(fmt.Sprintf(
		"%s - %s (%s %s)%s",
		title,
		episode.Marker(),
		source,
		resolution,
		extension,
	))
}

func (f MediaFilename) String() string {
	return string(f)
}
