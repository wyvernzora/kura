// Package planner snapshots backup state and deterministically assigns pending
// series to archive cartridges.
package planner

import (
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
	MetadataRef string
	Generation  int
}

// Series is the planner's immutable view of one tracked library series.
type Series struct {
	MetadataRef        string
	RootPath           string
	Generation         int
	PayloadFingerprint string
	Bytes              int64
	Eligibility        Eligibility
	Reason             Reason
	Detail             string
}

// LibrarySnapshot is a point-in-time view of tracked series.
type LibrarySnapshot struct {
	Series []Series
}

// Snapshot is one committed catalog entry.
type Snapshot struct {
	MetadataRef        string
	Generation         int
	TotalBytes         int64
	PayloadFingerprint string
}

// Volume is one active catalog volume and its observed capacity.
type Volume struct {
	VolumeID      volume.ID
	TapeID        tape.ID
	CapacityBytes int64
	FreeBytes     int64
	Snapshots     []Snapshot
}

// CatalogSnapshot is a point-in-time view of active catalog volumes.
type CatalogSnapshot struct {
	Volumes []Volume
}

// Blank declares one blank cartridge available to the consultation.
type Blank struct {
	TapeID tape.ID
}

// TargetKind distinguishes cataloged volumes from declared blank cartridges.
type TargetKind string

const (
	TargetKnown TargetKind = "known"
	TargetBlank TargetKind = "blank"
)

// Target identifies one allocation destination.
type Target struct {
	Kind          TargetKind
	VolumeID      volume.ID
	TapeID        tape.ID
	CapacityBytes int64
	FreeBytes     int64
}

// Assignment is the ordered roster for one target cartridge.
type Assignment struct {
	Target Target
	Series []Series
	Bytes  int64
}

// LineageRefusal reports a live generation below the cataloged maximum.
type LineageRefusal struct {
	Series                 Series
	CatalogedMaxGeneration int
}

// Sizing is the R19 capacity answer.
type Sizing struct {
	PendingBytes                int64
	KnownRoomBytes              int64
	DeclaredBlankRoomBytes      int64
	RosterBytes                 int64
	ShortfallBytes              int64
	NominalBlankCapacityBytes   int64
	NominalBlankUsableBytes     int64
	NominalBlankMediaGeneration string
	BringBlanks                 int
}

// InitPlanAwaitingApproval is the slice-5 status hook for persisted init
// drafts. The pure planner initializes the report field but does not populate
// it.
type InitPlanAwaitingApproval struct {
	PlanID  string
	TapeID  tape.ID
	Claimed []Pair
}

// Report is one deterministic consultation result.
type Report struct {
	Pending                   []Series
	Assignments               []Assignment
	Sizing                    Sizing
	Shortfall                 []Series
	Deferred                  []Series
	Unbackupable              []Series
	LineageRefusals           []LineageRefusal
	InitPlansAwaitingApproval []InitPlanAwaitingApproval
}
