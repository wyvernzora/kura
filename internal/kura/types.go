package kura

import (
	"time"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/resolve"
	seriespkg "github.com/wyvernzora/kura/internal/series"
)

type (
	SeriesRef   = refs.Series
	MetadataRef = refs.Metadata
	EpisodeRef  = refs.Episode
)

type (
	Resolution = resolve.Resolution
	Match      = resolve.Result
	Evidence   = resolve.Evidence

	ImportSkip = seriespkg.ImportSkip
)

type Episode struct {
	Number     int             `json:"number"`
	Media      MediaFile       `json:"media"`
	Companions []CompanionFile `json:"companions"`
}

type MediaFile struct {
	Path      string            `json:"path"`
	Source    string            `json:"source"`
	Size      int64             `json:"size"`
	MTime     string            `json:"mtime"`
	MediaInfo *domain.MediaInfo `json:"mediainfo,omitempty"`
}

type CompanionFile struct {
	Path     string `json:"path"`
	Role     string `json:"role,omitempty"`
	Language string `json:"language,omitempty"`
	Label    string `json:"label,omitempty"`
	Size     int64  `json:"size"`
	MTime    string `json:"mtime"`
}

type StagedEpisode struct {
	Season int `json:"season"`
	Number int `json:"number"`
	Episode
}

type ResolveInput struct {
	Terms []string
}

type AddInput struct {
	MetadataRef MetadataRef
	Ref         SeriesRef
}

type ImportInput struct {
	Ref         SeriesRef
	MetadataRef MetadataRef
}

type ScanInput struct {
	Replace bool
}

type ReadInput struct {
	Now time.Time
}

type StageInput struct {
	MediaPath  string
	Season     int
	Episode    int
	Source     string
	Companions []string
	Replace    bool
}

type ReconcileInput struct{}

type ScanResult struct {
	Series  SeriesRef        `json:"series"`
	Synced  []ScannedEpisode `json:"synced"`
	Skipped []ImportSkip     `json:"skipped"`
}

type SeriesRead struct {
	MetadataRef    MetadataRef  `json:"metadataRef"`
	Ref            SeriesRef    `json:"ref"`
	Root           string       `json:"root"`
	PreferredTitle string       `json:"preferredTitle"`
	CanonicalTitle string       `json:"canonicalTitle,omitempty"`
	Seasons        []SeasonRead `json:"seasons"`
}

type SeasonRead struct {
	MetadataRef MetadataRef   `json:"metadataRef,omitempty"`
	Number      int           `json:"number"`
	Episodes    []EpisodeRead `json:"episodes"`
}

type EpisodeRead struct {
	MetadataRef    MetadataRef   `json:"metadataRef,omitempty"`
	Season         int           `json:"season"`
	Number         int           `json:"number"`
	AbsoluteNumber *int          `json:"absoluteNumber,omitempty"`
	Aired          string        `json:"aired,omitempty"`
	Status         EpisodeStatus `json:"status"`
	Active         *EpisodeMedia `json:"active,omitempty"`
	Staged         *EpisodeMedia `json:"staged,omitempty"`
}

type EpisodeMedia struct {
	Source     string          `json:"source"`
	Resolution string          `json:"resolution,omitempty"`
	File       string          `json:"file"`
	Companions []CompanionFile `json:"companions"`
}

type EpisodeStatus string

const (
	EpisodeStatusPending     EpisodeStatus = "pending"
	EpisodeStatusMissing     EpisodeStatus = "missing"
	EpisodeStatusPresent     EpisodeStatus = "present"
	EpisodeStatusStaged      EpisodeStatus = "staged"
	EpisodeStatusUnavailable EpisodeStatus = "unavailable"
)

type ScannedEpisode struct {
	Status     ScanStatus `json:"status"`
	Season     int        `json:"season,omitempty"`
	Special    bool       `json:"special,omitempty"`
	Number     int        `json:"number"`
	Source     string     `json:"source"`
	Resolution string     `json:"resolution,omitempty"`
	Path       string     `json:"path"`
	Companions []string   `json:"companions"`
}

type ScanStatus string

const (
	ScanStatusNew      ScanStatus = "new"
	ScanStatusReplaced ScanStatus = "replaced"
	ScanStatusUpdated  ScanStatus = "updated"
	ScanStatusExisting ScanStatus = "existing"
)

type StageResult struct {
	Series   SeriesRef     `json:"series"`
	Applied  bool          `json:"applied"`
	Replaced bool          `json:"replaced"`
	Entry    StagedEpisode `json:"entry"`
}

type ReconcilePlan struct {
	Series    SeriesRef `json:"series"`
	FileTitle string    `json:"fileTitle"`
	Snapshot  string    `json:"snapshot"`
	Changes   []Change  `json:"changes"`
}

func (p ReconcilePlan) HasChanges() bool {
	return len(p.Changes) > 0
}

type FileMove struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Change struct {
	Kind    ChangeKind `json:"kind"`
	Season  int        `json:"season"`
	Episode int        `json:"episode"`
	FileMove

	Source     string     `json:"source,omitempty"`
	Resolution string     `json:"resolution,omitempty"`
	Companions []FileMove `json:"companions,omitempty"`
	Replaced   *Replaced  `json:"replaced,omitempty"`
}

func (c Change) Moves() []FileMove {
	moves := make([]FileMove, 0, 2+len(c.Companions))
	if c.Replaced != nil {
		moves = append(moves, c.Replaced.FileMove)
		moves = append(moves, c.Replaced.Companions...)
	}
	moves = append(moves, c.FileMove)
	moves = append(moves, c.Companions...)
	return moves
}

type ChangeKind string

const (
	ChangeAdd     ChangeKind = "add"
	ChangeMove    ChangeKind = "move"
	ChangeReplace ChangeKind = "replace"
)

type Replaced struct {
	FileMove
	Source     string     `json:"source,omitempty"`
	Resolution string     `json:"resolution,omitempty"`
	Companions []FileMove `json:"companions,omitempty"`
}

type ReconcileResult struct {
	Series       SeriesRef `json:"series"`
	AppliedMoves int       `json:"appliedMoves"`
}
