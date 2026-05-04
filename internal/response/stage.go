package response

// StageResult is workflow.Stage's response. The caller already knew
// the series and episode they passed in; success is implicit when
// the workflow returns without error. The fields surfaced are the
// ones the caller could not have known: whether the stage displaced
// an existing record, and the parsed mediainfo for the staged file.
type StageResult struct {
	Replaced bool      `json:"replaced"`
	Record   MediaShow `json:"record"`
}
