package rest

import (
	"net/http"
	"strconv"

	"github.com/wyvernzora/kura/internal/workflow"
)

// handleInboxList serves GET /api/v1/inbox.
//
// Query:
//
//	path           — slash-form directory or exact file under the inbox root (default: root)
//	recursive      — "1"/"true" to walk subtrees (default: false)
//	depth          — max levels when recursive (default 3, max 5)
//	limit          — max entries returned (default 500, max 5000)
//	kind           — "file" | "dir" | "symlink" filter
//	name_glob      — filepath.Match glob filtered against basename
//	include_hidden — "1"/"true" to surface dotfiles + .partial/.crdownload/...
//
// Response: response.InboxList, with content-derived ETag.
func (s *Server) handleInboxList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	in := workflow.InboxListInput{
		Path:          q.Get("path"),
		Recursive:     boolQuery(q.Get("recursive")),
		Kind:          q.Get("kind"),
		NameGlob:      q.Get("name_glob"),
		IncludeHidden: boolQuery(q.Get("include_hidden")),
	}

	if v := q.Get("depth"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, &validationError{msg: "depth must be a non-negative integer"})
			return
		}
		in.Depth = n
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, &validationError{msg: "limit must be a non-negative integer"})
			return
		}
		in.Limit = n
	}

	result, err := workflow.InboxList(r.Context(), s.deps.Workflow, in)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSONWithETag(w, r, http.StatusOK, result)
}

// boolQuery accepts the conventional truthy strings.
func boolQuery(v string) bool {
	switch v {
	case "1", "true", "TRUE", "yes", "on":
		return true
	}
	return false
}
