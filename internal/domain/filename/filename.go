// Package filename composes the canonical media filename basename from a
// series title plus episode and media facts. Pure derivation; no IO.
//
// Path-prefix construction lives in internal/storage/paths
// (paths.EpisodeMediaRel composes "Season N/<basename>"). This package
// owns the basename only.
package filename

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/textnorm"
)

// Title is a normalized series title safe to embed in a filename.
type Title struct {
	value textnorm.NFCString
}

// ParseTitle returns a Title after rejecting empty or filesystem-hostile
// inputs.
func ParseTitle(title string) (Title, error) {
	clean := CleanTitle(title)
	if clean.IsZero() {
		return Title{}, errors.New("filename: title is required")
	}
	if invalidTitle(clean.String()) {
		return Title{}, fmt.Errorf("filename: invalid title %q", title)
	}
	return clean, nil
}

// CleanTitle returns the NFC-normalized form of title without rejecting
// invalid characters. Use ParseTitle when validation is required.
func CleanTitle(title string) Title {
	return Title{value: textnorm.NFC(title)}
}

func (t Title) String() string {
	return t.value.String()
}

func (t Title) IsZero() bool {
	return t.value.IsZero()
}

func invalidTitle(title string) bool {
	return strings.ContainsFunc(title, func(r rune) bool {
		return r == '/' || r == '\\' || r == 0 || unicode.IsControl(r)
	})
}

// Media is the canonical media filename basename, e.g.
// "Bookworm - S01E01 (WebRip 1080p).mkv".
type Media string

// Facts captures the source/resolution shorthand embedded in the
// canonical filename.
type Facts struct {
	Source     media.Source
	Resolution media.Resolution
}

// BuildMedia composes the canonical basename from title + episode + facts
// + file extension.
func BuildMedia(title Title, episode refs.Episode, facts Facts, extension string) Media {
	source := facts.Source.Display()
	resolution := facts.Resolution.Display()
	if resolution == "" {
		resolution = "UnknownResolution"
	}
	return Media(fmt.Sprintf(
		"%s - %s (%s %s)%s",
		title,
		episode.Marker(),
		source,
		resolution,
		extension,
	))
}

func (m Media) String() string {
	return string(m)
}
