package mediainfo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/wyvernzora/kura/internal/domain/media"
)

var mediaFactsPattern = regexp.MustCompile(`\(([^()]*)\)\.[^.]+$`)

// Builder composes a media.Record from a path on disk: runs mediainfo,
// stats size + mtime, builds companion records.
type Builder struct {
	inspector media.Inspector
}

// NewBuilder returns a Builder backed by the supplied inspector.
func NewBuilder(inspector media.Inspector) Builder {
	return Builder{inspector: inspector}
}

type Input struct {
	MediaPath      string
	RecordPath     string
	Source         string
	CompanionPaths []CompanionInput
}

type CompanionInput struct {
	MediaPath  string
	RecordPath string
}

// Build runs mediainfo + stat against the inputs and returns the
// composed media.Record. RecordPath is the path that gets persisted in
// series.json (typically equal to MediaPath at stage time, or a
// canonical path during scan).
func (b Builder) Build(ctx context.Context, in Input) (media.Record, error) {
	info, err := b.inspector.Inspect(ctx, in.MediaPath)
	if err != nil {
		return media.Record{}, err
	}
	size, mtime, err := statFileFacts(in.MediaPath)
	if err != nil {
		return media.Record{}, err
	}
	source := in.Source
	if source == "" {
		source = InferSourceFromFilename(in.RecordPath)
	}
	// Mediainfo heuristic: filename inference often comes back
	// "unknown" (no parenthesized suffix or only a resolution token).
	// InferSourceFromMediainfo chains several mediainfo-derived
	// signals (embedded title text, container extension, audio
	// codec) to fill the gap. Only consults this fallback when
	// the stronger filename signal failed — never overrides.
	if source == "" || source == media.SourceUnknown.String() {
		if hint := InferSourceFromMediainfo(info, in.RecordPath); hint != "" {
			source = hint
		}
	}
	resolution, err := media.ParseResolution(info.Resolution)
	if err != nil {
		return media.Record{}, err
	}
	record := media.Record{
		Path:       in.RecordPath,
		Source:     media.ParseSource(source),
		Resolution: resolution,
		Codec:      media.ParseCodec(info.VideoCodec),
		Size:       size,
		MTime:      mtime,
		Companions: []media.Companion{},
	}
	for _, companion := range in.CompanionPaths {
		size, mtime, err := statFileFacts(companion.MediaPath)
		if err != nil {
			return media.Record{}, err
		}
		record.Companions = append(record.Companions, media.Companion{
			Path:  companion.RecordPath,
			Size:  size,
			MTime: mtime,
		})
	}
	return record, nil
}

func statFileFacts(path string) (int64, time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, time.Time{}, err
	}
	if info.IsDir() {
		return 0, time.Time{}, fmt.Errorf("mediainfo: %q is a directory", path)
	}
	return info.Size(), info.ModTime().UTC().Truncate(time.Second), nil
}

// InferSourceFromFilename pulls the source token out of a canonical
// filename like "Foo - S01E01 (WebRip 1080p).mkv". Walks the suffix
// tokens and returns the first one media.IsKnown recognizes; falls
// back to "unknown" when the suffix has only a resolution / codec /
// other non-source token (e.g. "(1280x720)" → unknown).
func InferSourceFromFilename(path string) string {
	name := filepath.ToSlash(path)
	matches := mediaFactsPattern.FindStringSubmatch(name)
	if len(matches) != 2 {
		return "unknown"
	}
	for field := range strings.FieldsSeq(matches[1]) {
		if media.IsKnown(field) {
			return field
		}
	}
	return "unknown"
}

// textTokenSplit splits free text on the punctuation common to
// scene-style names ("Foo.S01E01.BluRay.1080p-Group", "Foo [BluRay
// 1080p]", etc.) so each token can be tested against media.IsKnown.
// Hyphen is intentionally NOT a separator: "WEB-DL" and "BD-Rip" are
// single source tokens. Trailing -Group suffixes still split off via
// the surrounding dot in scene names like "1080p-Group", or are
// harmlessly ignored when not IsKnown.
var textTokenSplit = regexp.MustCompile(`[\s._\[\]()/+,]+`)

// InferSourceFromMediainfo chains mediainfo-derived heuristics to
// guess a source when filename inference fails. Signals are tried
// in decreasing reliability order; the first match wins. Returns
// "" when nothing matches.
//
//  1. Container's General.Title field (free-text scan).
//  2. Container extension — only the unambiguous ones (m2ts, mts,
//     webm). Generic containers (.mkv, .mp4, .ts, .avi) deliberately
//     return nothing because they cross multiple sources.
//  3. Audio codec — disc-only formats (TrueHD/MLP, DTS-HD MA, DTS:X)
//     imply BluRay; web/broadcast codecs (AC-3, AAC, Opus) cross
//     multiple sources and are not used.
//
// Used as a heuristic fallback only; caller must check stronger
// signals (caller-provided override, filename suffix) first.
func InferSourceFromMediainfo(info media.Info, mediaPath string) string {
	if hint := InferSourceFromText(info.Title); hint != "" {
		return hint
	}
	if hint := sourceFromContainerExt(mediaPath); hint != "" {
		return hint
	}
	if hint := sourceFromAudioCodec(info.AudioCodec); hint != "" {
		return hint
	}
	return ""
}

// sourceFromContainerExt maps unambiguous container extensions to
// their implied source. Generic containers return "" — they say
// nothing about source.
func sourceFromContainerExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".m2ts", ".mts":
		// BDAV stream files: only produced by BluRay rips / playback.
		return media.SourceBluRay.String()
	case ".webm":
		// Web-native container; never appears off the open web.
		return media.SourceWebRip.String()
	}
	return ""
}

// sourceFromAudioCodec maps disc-only audio formats to BluRay.
// Conservative: only formats that are licensing-locked or
// bitrate-prohibitive outside disc media are listed. Web releases
// can carry Atmos via E-AC-3-JOC but the underlying stream type
// is E-AC-3, not TrueHD/DTS-HD, so this stays disc-side.
func sourceFromAudioCodec(codec string) string {
	norm := strings.ToLower(strings.TrimSpace(codec))
	norm = strings.ReplaceAll(norm, " ", "")
	norm = strings.ReplaceAll(norm, "-", "")
	switch {
	// Dolby TrueHD (mediainfo emits "MLP FBA" for the underlying
	// Meridian Lossless Packing stream, sometimes "TrueHD").
	case strings.Contains(norm, "truehd"), strings.Contains(norm, "mlpfba"):
		return media.SourceBluRay.String()
	// DTS-HD MA / DTS:X variants.
	case strings.Contains(norm, "dtshd"),
		strings.Contains(norm, "dtsma"),
		strings.Contains(norm, "dtsxll"),
		strings.Contains(norm, "dtsx"):
		return media.SourceBluRay.String()
	}
	return ""
}

// InferSourceFromText scans free text (typically the container's
// General.Title field) for an *informative* source token — a
// media.IsKnown value that isn't itself "unknown". Returns the first
// such match in token order, or "" when nothing useful is found.
//
// Used as a heuristic fallback only; caller must check stronger
// signals (caller-provided override, filename suffix) first. The
// "skip Unknown literal" rule matters because some encoders write
// the literal string "Unknown" into General.Title and IsKnown's
// switch happily matches SourceUnknown — without the skip, the
// fallback would replace the incoming "unknown" with another
// "unknown" and pretend something improved.
func InferSourceFromText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	for _, token := range textTokenSplit.Split(text, -1) {
		if token == "" {
			continue
		}
		if media.ParseSource(token) == media.SourceUnknown {
			continue
		}
		if media.IsKnown(token) {
			return token
		}
	}
	return ""
}

var resolutionDimsPattern = regexp.MustCompile(`(?i)\b(\d{3,4})x(\d{3,4})\b`)
var resolutionTagPattern = regexp.MustCompile(`(?i)\b(2160p|1440p|1080p|720p|480p|360p|4k)\b`)

// InferResolutionFromFilename pulls a resolution out of the parenthesized
// suffix of a canonical filename. Returns the canonical "WxH" form
// (e.g. "1920x1080") so callers can media.ParseResolution it. Empty
// string when no recognized token is present.
//
// Recognizes both explicit dimensions ("1920x1080", "1280x720") and
// shorthand tags ("1080p", "720p", "4K"). Shorthand maps to the
// standard pixel counts media.Resolution.Display() emits.
func InferResolutionFromFilename(path string) string {
	name := filepath.ToSlash(path)
	matches := mediaFactsPattern.FindStringSubmatch(name)
	if len(matches) != 2 {
		return ""
	}
	suffix := matches[1]
	if dims := resolutionDimsPattern.FindStringSubmatch(suffix); len(dims) == 3 {
		return dims[1] + "x" + dims[2]
	}
	if tag := resolutionTagPattern.FindString(suffix); tag != "" {
		switch strings.ToLower(tag) {
		case "4k", "2160p":
			return "3840x2160"
		case "1440p":
			return "2560x1440"
		case "1080p":
			return "1920x1080"
		case "720p":
			return "1280x720"
		case "480p":
			return "854x480"
		case "360p":
			return "640x360"
		}
	}
	return ""
}

// RecognizedVideoFile reports whether path's extension is a video format
// Kura knows how to track.
func RecognizedVideoFile(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	return slices.Contains([]string{
		".mkv",
		".mp4",
		".m4v",
		".avi",
		".mov",
		".webm",
		".ts",
		".m2ts",
		".wmv",
		".ogm",
		".ogv",
	}, extension)
}
