// Package api defines the public tape-backup REST wire shapes consumed by
// suite clients.
package api

import "time"

// Blank declares one blank cartridge available to a consultation.
type Blank struct {
	TapeID string `json:"tapeID"`
}

// PlanRequest selects the loaded cartridge for a targeted tape operation.
type PlanRequest struct {
	TapeID string `json:"tapeID,omitempty"`
}

// Pair identifies one series generation.
type Pair struct {
	MetadataRef string `json:"metadataRef"`
	Generation  int    `json:"generation"`
}

// Series is one series in a consultation report.
type Series struct {
	MetadataRef        string `json:"metadataRef"`
	RootPath           string `json:"rootPath"`
	Generation         int    `json:"generation"`
	PayloadFingerprint string `json:"payloadFingerprint"`
	Bytes              int64  `json:"bytes"`
	Eligibility        string `json:"eligibility"`
	Reason             string `json:"reason,omitempty"`
	Detail             string `json:"detail,omitempty"`
}

// Target identifies one consultation assignment destination.
type Target struct {
	Kind          string `json:"kind"`
	VolumeID      string `json:"volumeID,omitempty"`
	TapeID        string `json:"tapeID"`
	CapacityBytes int64  `json:"capacityBytes"`
	FreeBytes     int64  `json:"freeBytes"`
}

// Assignment is the ordered consultation roster for one cartridge.
type Assignment struct {
	Target Target   `json:"target"`
	Series []Series `json:"series"`
	Bytes  int64    `json:"bytes"`
}

// LineageRefusal reports a live generation below the cataloged maximum.
type LineageRefusal struct {
	Series                 Series `json:"series"`
	CatalogedMaxGeneration int    `json:"catalogedMaxGeneration"`
}

// Sizing is the consultation capacity answer.
type Sizing struct {
	PendingBytes                int64  `json:"pendingBytes"`
	KnownRoomBytes              int64  `json:"knownRoomBytes"`
	DeclaredBlankRoomBytes      int64  `json:"declaredBlankRoomBytes"`
	RosterBytes                 int64  `json:"rosterBytes"`
	ShortfallBytes              int64  `json:"shortfallBytes"`
	NominalBlankCapacityBytes   int64  `json:"nominalBlankCapacityBytes"`
	NominalBlankUsableBytes     int64  `json:"nominalBlankUsableBytes"`
	NominalBlankMediaGeneration string `json:"nominalBlankMediaGeneration,omitempty"`
	BringBlanks                 int    `json:"bringBlanks"`
}

// InitPlanAwaitingApproval is one persisted init draft visible in status.
type InitPlanAwaitingApproval struct {
	PlanID     string    `json:"planID"`
	TapeID     string    `json:"tapeID"`
	CreatedAt  time.Time `json:"createdAt"`
	AgeSeconds int64     `json:"ageSeconds"`
	Claimed    []Pair    `json:"claimed"`
}

// Report is the consultation sizing and assignment report.
type Report struct {
	Pending                   []Series                   `json:"pending"`
	Assignments               []Assignment               `json:"assignments"`
	Sizing                    Sizing                     `json:"sizing"`
	Shortfall                 []Series                   `json:"shortfall"`
	Deferred                  []Series                   `json:"deferred"`
	Unbackupable              []Series                   `json:"unbackupable"`
	LineageRefusals           []LineageRefusal           `json:"lineageRefusals"`
	InitPlansAwaitingApproval []InitPlanAwaitingApproval `json:"initPlansAwaitingApproval"`
}

// Creator records the build and host that authored a plan.
type Creator struct {
	Version string `json:"version"`
	Host    string `json:"host"`
}

// PlanTarget identifies the cartridge named by a plan.
type PlanTarget struct {
	TapeID        string `json:"tapeID,omitempty"`
	MediumSerial  string `json:"mediumSerial,omitempty"`
	VolumeID      string `json:"volumeID,omitempty"`
	UsedBytes     int64  `json:"usedBytes,omitempty"`
	FreeBytes     int64  `json:"freeBytes,omitempty"`
	CapacityBytes int64  `json:"capacityBytes,omitempty"`
}

// Action is one ordered operation or assertion in a plan.
type Action struct {
	Type               string    `json:"type"`
	MetadataRef        *string   `json:"metadataRef,omitempty"`
	RootPath           *string   `json:"rootPath,omitempty"`
	Generation         *int      `json:"generation,omitempty"`
	PayloadFingerprint *string   `json:"payloadFingerprint,omitempty"`
	Bytes              *int64    `json:"bytes,omitempty"`
	VolumeID           *string   `json:"volumeID,omitempty"`
	Snapshots          *[]string `json:"snapshots,omitempty"`
}

// Plan is immutable ordered intent for one cartridge.
type Plan struct {
	SchemaVersion int        `json:"schemaVersion"`
	PlanID        string     `json:"planID"`
	CreatedAt     time.Time  `json:"createdAt"`
	CreatedBy     Creator    `json:"createdBy"`
	Target        PlanTarget `json:"target"`
	Actions       []Action   `json:"actions"`
}

// ConsultResult is the persisted sizing report and draft roster.
type ConsultResult struct {
	SchemaVersion int       `json:"schemaVersion"`
	CreatedAt     time.Time `json:"createdAt"`
	Report        Report    `json:"report"`
	Drafts        []Plan    `json:"drafts"`
}

// LivePlan is one registered plan in a running session.
type LivePlan struct {
	PlanID string `json:"planID"`
	TapeID string `json:"tapeID"`
}

// LiveSession is one running tape session.
type LiveSession struct {
	SessionID string     `json:"sessionID"`
	StartedAt time.Time  `json:"startedAt"`
	Plans     []LivePlan `json:"plans"`
}

// DriveStatus is a one-shot drive availability observation.
type DriveStatus struct {
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

// StatusResult is the read-only tape status response.
type StatusResult struct {
	LiveSessions []LiveSession              `json:"liveSessions"`
	Drive        DriveStatus                `json:"drive"`
	InitDrafts   []InitPlanAwaitingApproval `json:"initDrafts"`
	Consult      *ConsultResult             `json:"consult,omitempty"`
}

// PlanResult is a targeted decision-tree preview.
type PlanResult struct {
	Classification string     `json:"classification"`
	Plan           Plan       `json:"plan"`
	Target         PlanTarget `json:"target"`
	Persisted      bool       `json:"persisted"`
	Debris         []string   `json:"debris"`
}

// RunResult identifies the exact plan executed by a run request.
type RunResult struct {
	Classification string `json:"classification"`
	Plan           Plan   `json:"plan"`
}
