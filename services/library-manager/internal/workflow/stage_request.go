package workflow

import (
	"fmt"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/media"
	domainrefs "github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/selector"
)

// StageRequest is the transport-neutral string-input shape every
// caller (REST handler, MCP tool handler, future transports) maps
// onto before invoking BuildStageInput. Carrying string-typed fields
// lets the per-transport wire types stay free of selector.Path /
// refs.Episode dependencies, and centralizes the marker + selector
// parsing in one place.
type StageRequest struct {
	Episodes []StageRequestEpisode
	Trash    []StageRequestTrash
	Extras   []StageRequestExtra
}

// StageRequestEpisode mirrors EpisodeStageItem with all-string fields.
// Episode is an episode marker (e.g. "S01E03"). Media + Companions
// are inbox: selectors.
type StageRequestEpisode struct {
	Episode    string
	Media      string
	Source     string
	Companions []string
	Replace    bool
	Attrs      map[string]string
}

// StageRequestTrash mirrors TrashStageItem with all-string fields.
// Path + Companions are series: selectors.
type StageRequestTrash struct {
	Path       string
	Companions []string
}

// StageRequestExtra mirrors ExtraStageItem with all-string fields.
// Source is an inbox: selector.
type StageRequestExtra struct {
	Season int
	Source string
	Prefix string
}

// StageRequestError is returned by BuildStageInput when an axis entry
// fails parse-time validation. Axis is one of "episodes", "trash",
// "extras"; Index is the array position; Field is the offending field
// name (or "companions[J]" for sub-loop entries); Cause is the
// underlying parser error or empty for required-field-missing
// rejections.
type StageRequestError struct {
	Axis  string
	Index int
	Field string
	Cause error
	// Message overrides the default rendering for cases where the
	// underlying Cause is nil (e.g. required-field-missing).
	Message string
}

func (e *StageRequestError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s[%d].%s: %s", e.Axis, e.Index, e.Field, e.Message)
	}
	return fmt.Sprintf("%s[%d].%s: %v", e.Axis, e.Index, e.Field, e.Cause)
}

func (e *StageRequestError) Unwrap() error { return e.Cause }

// BuildStageInput parses a transport-neutral StageRequest into the
// strongly-typed StageInput the Stage workflow consumes. The Ref
// field on the returned StageInput is left unset — callers fill it
// in after their own series-ref lookup so parse-time failures don't
// require a successful Index.Get to surface. Returns a
// *StageRequestError on parse failure so transports can render the
// error in their own wire form.
func BuildStageInput(req StageRequest) (StageInput, error) {
	if len(req.Episodes) == 0 && len(req.Trash) == 0 && len(req.Extras) == 0 {
		return StageInput{}, &StageRequestError{
			Axis:    "episodes",
			Index:   -1,
			Field:   "",
			Message: "at least one of episodes, trash, or extras is required",
		}
	}
	episodes, err := parseStageRequestEpisodes(req.Episodes)
	if err != nil {
		return StageInput{}, err
	}
	trash, err := parseStageRequestTrash(req.Trash)
	if err != nil {
		return StageInput{}, err
	}
	extras, err := parseStageRequestExtras(req.Extras)
	if err != nil {
		return StageInput{}, err
	}
	return StageInput{
		Episodes: episodes,
		Trash:    trash,
		Extras:   extras,
	}, nil
}

func parseStageRequestEpisodes(items []StageRequestEpisode) ([]EpisodeStageItem, error) {
	out := make([]EpisodeStageItem, 0, len(items))
	for index, item := range items {
		episode, err := domainrefs.ParseEpisodeMarker(item.Episode)
		if err != nil {
			return nil, &StageRequestError{Axis: "episodes", Index: index, Field: "episode", Cause: err}
		}
		if item.Media == "" {
			return nil, &StageRequestError{
				Axis: "episodes", Index: index, Field: "media",
				Message: "is required (an inbox: selector for normal stage, or a series: selector for in-place metadata override on the active record)",
			}
		}
		mediaSel, err := selector.Parse(item.Media)
		if err != nil {
			return nil, &StageRequestError{Axis: "episodes", Index: index, Field: "media", Cause: err}
		}
		if mediaSel.Scheme != selector.Inbox && mediaSel.Scheme != selector.Series {
			return nil, &StageRequestError{
				Axis: "episodes", Index: index, Field: "media",
				Message: fmt.Sprintf("expected inbox: or series: scheme, got %q", mediaSel.Scheme),
			}
		}
		companions, err := parseStageRequestCompanions(item.Companions, "episodes", index, selector.ParseInbox)
		if err != nil {
			return nil, err
		}
		attrs := media.Attrs(item.Attrs)
		if err := media.ValidateAttrs(attrs); err != nil {
			return nil, &StageRequestError{Axis: "episodes", Index: index, Field: "attrs", Cause: err}
		}
		out = append(out, EpisodeStageItem{
			Episode:    episode,
			Media:      mediaSel,
			Source:     item.Source,
			Companions: companions,
			Replace:    item.Replace,
			Attrs:      media.CloneAttrs(attrs),
		})
	}
	return out, nil
}

func parseStageRequestTrash(items []StageRequestTrash) ([]TrashStageItem, error) {
	out := make([]TrashStageItem, 0, len(items))
	for index, item := range items {
		if item.Path == "" {
			return nil, &StageRequestError{
				Axis: "trash", Index: index, Field: "path",
				Message: "is required (a series: selector — relative to the series root)",
			}
		}
		pathSel, err := selector.ParseSeries(item.Path)
		if err != nil {
			return nil, &StageRequestError{Axis: "trash", Index: index, Field: "path", Cause: err}
		}
		companions, err := parseStageRequestCompanions(item.Companions, "trash", index, selector.ParseSeries)
		if err != nil {
			return nil, err
		}
		out = append(out, TrashStageItem{Path: pathSel, Companions: companions})
	}
	return out, nil
}

func parseStageRequestExtras(items []StageRequestExtra) ([]ExtraStageItem, error) {
	out := make([]ExtraStageItem, 0, len(items))
	for index, item := range items {
		if item.Source == "" {
			return nil, &StageRequestError{
				Axis: "extras", Index: index, Field: "source",
				Message: "is required (an inbox: selector — see kura_inbox_list)",
			}
		}
		sourceSel, err := selector.ParseInbox(item.Source)
		if err != nil {
			return nil, &StageRequestError{Axis: "extras", Index: index, Field: "source", Cause: err}
		}
		out = append(out, ExtraStageItem{
			Season: item.Season,
			Source: sourceSel,
			Prefix: item.Prefix,
		})
	}
	return out, nil
}

// parseStageRequestCompanions handles the per-item companion slice.
// parse selects the correct selector parser (Inbox for episodes,
// Series for trash). axis + parentIdx populate the error message so
// the caller knows which slot tripped.
func parseStageRequestCompanions(
	raws []string,
	axis string,
	parentIdx int,
	parse func(string) (selector.Path, error),
) ([]selector.Path, error) {
	out := make([]selector.Path, 0, len(raws))
	for j, raw := range raws {
		sel, err := parse(raw)
		if err != nil {
			return nil, &StageRequestError{
				Axis:  axis,
				Index: parentIdx,
				Field: fmt.Sprintf("companions[%d]", j),
				Cause: err,
			}
		}
		out = append(out, sel)
	}
	return out, nil
}
