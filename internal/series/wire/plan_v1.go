package wire

type ReconcilePlanRecordV1 struct {
	Type          string          `json:"type"`
	SchemaVersion int             `json:"schemaVersion"`
	Token         string          `json:"token"`
	CreatedAt     string          `json:"createdAt"`
	ExpiresAt     string          `json:"expiresAt"`
	Series        string          `json:"series"`
	MetadataRef   string          `json:"metadataRef"`
	Plan          ReconcilePlanV1 `json:"plan"`
}

type ReconcilePlanV1 struct {
	Series    string              `json:"series"`
	FileTitle string              `json:"fileTitle"`
	Snapshot  string              `json:"snapshot"`
	Changes   []ReconcileChangeV1 `json:"changes"`
}

type ReconcileChangeV1 struct {
	Kind       string                `json:"kind"`
	Episode    string                `json:"episode"`
	From       string                `json:"from"`
	To         string                `json:"to"`
	Source     string                `json:"source,omitempty"`
	Resolution string                `json:"resolution,omitempty"`
	Companions []ReconcileFileMoveV1 `json:"companions,omitempty"`
	Replaced   *ReconcileReplacedV1  `json:"replaced,omitempty"`
}

type ReconcileFileMoveV1 struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type ReconcileReplacedV1 struct {
	From       string                `json:"from"`
	To         string                `json:"to"`
	Source     string                `json:"source,omitempty"`
	Resolution string                `json:"resolution,omitempty"`
	Companions []ReconcileFileMoveV1 `json:"companions,omitempty"`
}

type ReconcileEventRecordV1 struct {
	Type          string              `json:"type"`
	SchemaVersion int                 `json:"schemaVersion"`
	At            string              `json:"at"`
	Phase         string              `json:"phase"`
	Index         int                 `json:"index"`
	Total         int                 `json:"total"`
	Move          ReconcileFileMoveV1 `json:"move"`
	Error         string              `json:"error,omitempty"`
}

type ReconcileResultRecordV1 struct {
	Type          string `json:"type"`
	SchemaVersion int    `json:"schemaVersion"`
	At            string `json:"at"`
	Status        string `json:"status"`
	AppliedMoves  int    `json:"appliedMoves,omitempty"`
	Error         string `json:"error,omitempty"`
}
