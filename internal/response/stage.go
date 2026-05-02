package response

import "github.com/wyvernzora/kura/internal/domain/refs"

// StageResult is workflow.Stage's response. Replaced reports whether
// the operation displaced an existing active or staged record (always
// true when in.Replace was set on a slot that already had something).
type StageResult struct {
	Series   refs.Series  `json:"series"`
	Applied  bool         `json:"applied"`
	Replaced bool         `json:"replaced"`
	Episode  refs.Episode `json:"episode"`
	Record   MediaShow    `json:"record"`
}
