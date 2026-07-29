// Package planner snapshots backup state and deterministically assigns pending
// series to archive cartridges.
package planner

import (
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

// Eligibility is the result of applying the v1 archive eligibility policy.
type Eligibility string

const (
	EligibilityEligible     Eligibility = "eligible"
	EligibilityDeferred     Eligibility = "deferred"
	EligibilityUnbackupable Eligibility = "unbackupable"
)

// Reason identifies why a series is not eligible this consultation.
type Reason string

const (
	ReasonStagedIntent  Reason = "staged_intent"
	ReasonActiveClaim   Reason = "active_claim"
	ReasonSymlink       Reason = "symlink"
	ReasonNonRegular    Reason = "non_regular_file"
	ReasonNonNFCPath    Reason = "non_nfc_path"
	ReasonTooLarge      Reason = "too_large"
	ReasonInvalidAction Reason = "invalid_backup_action"
)

// Pair is the permanent identity of one series snapshot.
type Pair struct {
	MetadataRef string `json:"metadataRef"`
	Generation  int    `json:"generation"`
}

// Series is the planner's immutable view of one tracked library series.
type Series struct {
	MetadataRef        string      `json:"metadataRef"`
	RootPath           string      `json:"rootPath"`
	Generation         int         `json:"generation"`
	PayloadFingerprint string      `json:"payloadFingerprint"`
	Bytes              int64       `json:"bytes"`
	Eligibility        Eligibility `json:"eligibility"`
	Reason             Reason      `json:"reason,omitempty"`
	Detail             string      `json:"detail,omitempty"`
}

// LibrarySnapshot is a point-in-time view of tracked series.
type LibrarySnapshot struct {
	Series []Series `json:"series"`
}

// Snapshot is one committed catalog entry.
type Snapshot struct {
	MetadataRef        string `json:"metadataRef"`
	Generation         int    `json:"generation"`
	TotalBytes         int64  `json:"totalBytes"`
	PayloadFingerprint string `json:"payloadFingerprint"`
}

// Volume is one active catalog volume and its observed capacity.
type Volume struct {
	VolumeID      volume.ID  `json:"volumeID"`
	TapeID        tape.ID    `json:"tapeID"`
	CapacityBytes int64      `json:"capacityBytes"`
	FreeBytes     int64      `json:"freeBytes"`
	Snapshots     []Snapshot `json:"snapshots"`
}

// CatalogSnapshot is a point-in-time view of active catalog volumes.
type CatalogSnapshot struct {
	Volumes []Volume `json:"volumes"`
}

// Blank declares one blank cartridge available to the consultation.
type Blank struct {
	TapeID tape.ID `json:"tapeID"`
}

// TargetKind distinguishes cataloged volumes from declared blank cartridges.
type TargetKind string

const (
	TargetKnown TargetKind = "known"
	TargetBlank TargetKind = "blank"
)

// Target identifies one allocation destination.
type Target struct {
	Kind          TargetKind `json:"kind"`
	VolumeID      volume.ID  `json:"volumeID,omitempty"`
	TapeID        tape.ID    `json:"tapeID"`
	CapacityBytes int64      `json:"capacityBytes"`
	FreeBytes     int64      `json:"freeBytes"`
}

// Assignment is the ordered roster for one target cartridge.
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

// Sizing is the R19 capacity answer.
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

// InitPlanAwaitingApproval is the slice-5 status hook for persisted init
// drafts. The pure planner initializes the report field but does not populate
// it.
type InitPlanAwaitingApproval struct {
	PlanID     string    `json:"planID"`
	TapeID     tape.ID   `json:"tapeID"`
	CreatedAt  time.Time `json:"createdAt"`
	AgeSeconds int64     `json:"ageSeconds"`
	Claimed    []Pair    `json:"claimed"`
}

// Report is one deterministic consultation result.
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
