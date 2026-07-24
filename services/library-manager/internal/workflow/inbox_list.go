package workflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/wyvernzora/kura/services/library-manager/internal/inbox"
	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
)

// InboxListInput parameters for the InboxList workflow.
type InboxListInput struct {
	Path          string
	Recursive     bool
	Depth         int
	Limit         int
	Kind          string
	NameGlob      string
	IncludeHidden bool
}

const (
	// inboxListDefaultLimit applies when caller passes Limit==0.
	inboxListDefaultLimit = 500
	// inboxListMaxLimit caps user-supplied Limit.
	inboxListMaxLimit = 5000
	// inboxListDefaultDepth applies when Recursive=true and Depth<=0.
	inboxListDefaultDepth = 3
	// inboxListMaxDepth caps user-supplied Depth.
	inboxListMaxDepth = 5
)

// InboxLimitTooLargeError signals Limit exceeds the cap.
type InboxLimitTooLargeError struct {
	Limit int
	Max   int
}

func (e *InboxLimitTooLargeError) Error() string {
	return fmt.Sprintf("workflow: inbox list limit %d exceeds maximum %d", e.Limit, e.Max)
}

// InboxDepthTooLargeError signals Depth exceeds the cap.
type InboxDepthTooLargeError struct {
	Depth int
	Max   int
}

func (e *InboxDepthTooLargeError) Error() string {
	return fmt.Sprintf("workflow: inbox list depth %d exceeds maximum %d", e.Depth, e.Max)
}

// InboxNotConfiguredError signals library.inbox was not configured.
type InboxNotConfiguredError struct{}

func (e *InboxNotConfiguredError) Error() string {
	return "workflow: inbox is not configured (library.inbox is empty)"
}

// InboxList enumerates deps.InboxRoot/<in.Path>. Directory paths list
// their children; file paths return that exact entry. Limit and depth
// are clamped to defaults when zero, rejected when above configured
// maxima.
func InboxList(ctx context.Context, deps Deps, in InboxListInput) (api.InboxList, error) {
	_ = ctx
	if deps.InboxRoot == "" {
		return api.InboxList{}, &InboxNotConfiguredError{}
	}

	limit := in.Limit
	if limit == 0 {
		limit = inboxListDefaultLimit
	}
	if limit < 0 {
		return api.InboxList{}, fmt.Errorf("workflow: inbox list limit must be >= 0 (got %d)", limit)
	}
	if limit > inboxListMaxLimit {
		return api.InboxList{}, &InboxLimitTooLargeError{Limit: in.Limit, Max: inboxListMaxLimit}
	}

	depth := in.Depth
	if in.Recursive && depth == 0 {
		depth = inboxListDefaultDepth
	}
	if depth < 0 {
		return api.InboxList{}, fmt.Errorf("workflow: inbox list depth must be >= 0 (got %d)", depth)
	}
	if depth > inboxListMaxDepth {
		return api.InboxList{}, &InboxDepthTooLargeError{Depth: in.Depth, Max: inboxListMaxDepth}
	}

	res, err := inbox.Walk(deps.InboxRoot, inbox.Options{
		Path:          in.Path,
		Recursive:     in.Recursive,
		Depth:         depth,
		Limit:         limit,
		Kind:          in.Kind,
		NameGlob:      in.NameGlob,
		IncludeHidden: in.IncludeHidden,
	})
	if err != nil {
		return api.InboxList{}, err
	}

	out := api.InboxList{
		Path:        inboxSelector(deps.InboxRoot, res.Path),
		Entries:     make([]api.InboxEntry, len(res.Entries)),
		Truncated:   res.Truncated,
		ElidedCount: res.ElidedCount,
	}
	for i, e := range res.Entries {
		entry := api.InboxEntry{
			Path: inboxSelector(deps.InboxRoot, e.RelPath),
			Kind: string(e.Kind),
			Size: e.Size,
		}
		if !e.MTime.IsZero() {
			entry.MTime = e.MTime.UTC().Format("2006-01-02T15:04:05Z")
		}
		if e.SymlinkTarget != "" {
			entry.SymlinkTarget = e.SymlinkTarget
		}
		out.Entries[i] = entry
	}

	if out.Truncated {
		out.Hint = inboxListHint(in)
	}
	return out, nil
}

// inboxListHint suggests narrowing knobs the caller didn't already
// engage. Hint lines are plain prose; renderers (CLI table footer, MCP
// plain text) format them with their own decoration.
func inboxListHint(in InboxListInput) []string {
	var hints []string
	if in.Path == "" {
		hints = append(hints, `pass path="<subdir>" to list a single subtree`)
	}
	if in.Recursive {
		hints = append(hints, "drop recursive to list only the immediate children")
	} else {
		hints = append(hints, "set recursive=true with depth to walk subtrees")
	}
	if in.NameGlob == "" {
		hints = append(hints, `add nameGlob="*.mkv" (or similar) to filter by extension`)
	}
	if in.Kind == "" {
		hints = append(hints, `add kind="file" or kind="dir" to restrict by entry type`)
	}
	if in.Limit < inboxListMaxLimit {
		hints = append(hints, fmt.Sprintf("raise limit (max %d) if you really need everything", inboxListMaxLimit))
	}
	return hints
}

// IsInboxNotConfigured reports whether err is an InboxNotConfiguredError.
// Helps surfaces (REST/MCP) generate consistent 503-ish responses.
func IsInboxNotConfigured(err error) bool {
	var e *InboxNotConfiguredError
	return errors.As(err, &e)
}
