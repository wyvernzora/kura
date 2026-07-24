package render

import (
	"fmt"
	"strings"

	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
)

// InboxList formats an inbox listing as plain text suitable for an MCP
// tool result content block. Format mirrors the design in the plan:
//
//	F   1.15GB  2026-05-01T03:14Z  [BDrip] Hoshi.../E01.mkv
//	D        -  2026-05-01T03:14Z  [BDrip] Hoshi.../Subs/
//	L        -  2026-04-22T18:02Z  link
//	# symlink link -> /elsewhere
//
// Trailing slash on directory paths. Symlink targets are emitted as hint
// lines so the data row's path column remains a copyable selector.
// Hint lines (when truncated) follow as ' #'-prefixed lines so callers
// can distinguish data from advice.
func InboxList(r api.InboxList) string {
	if len(r.Entries) == 0 && !r.Truncated {
		return "(empty)\n"
	}

	var b strings.Builder
	for _, e := range r.Entries {
		kind := kindGlyph(e.Kind)
		size := "-"
		if e.Kind == "file" {
			size = HumanByteSize(e.Size)
		}
		mtime := e.MTime
		if mtime == "" {
			mtime = "-"
		} else {
			// Workflow emits "2006-01-02T15:04:05Z"; truncate to
			// minute precision for display.
			mtime = truncateToMinuteUTC(mtime)
		}
		path := e.Path
		if e.Kind == "dir" && !strings.HasSuffix(path, "/") {
			path += "/"
		}
		fmt.Fprintf(&b, "%s   %7s  %s  %s\n", kind, size, mtime, path)
		if e.Kind == "symlink" && e.SymlinkTarget != "" {
			fmt.Fprintf(&b, "# symlink %s -> %s\n", e.Path, e.SymlinkTarget)
		}
	}

	if r.Truncated {
		fmt.Fprintf(&b, "... [%d entries shown above; %d entries elided]\n", len(r.Entries), r.ElidedCount)
		if len(r.Hint) > 0 {
			b.WriteString("# Narrow scope to retry:\n")
			for _, h := range r.Hint {
				fmt.Fprintf(&b, "#   %s\n", h)
			}
		}
	}
	return b.String()
}

// truncateToMinuteUTC reduces "2006-01-02T15:04:05Z" to
// "2006-01-02T15:04Z". Returns the input unchanged when it doesn't fit
// the expected shape — workflow always emits UTC, but this avoids
// mangling odd inputs.
func truncateToMinuteUTC(s string) string {
	const expected = len("2006-01-02T15:04:05Z")
	if len(s) != expected {
		return s
	}
	return s[:16] + "Z"
}

func kindGlyph(kind string) string {
	switch kind {
	case "file":
		return "F"
	case "dir":
		return "D"
	case "symlink":
		return "L"
	default:
		return "?"
	}
}
