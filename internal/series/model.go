package series

import (
	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/series/layout"
	"github.com/wyvernzora/kura/internal/series/state"
	"github.com/wyvernzora/kura/internal/textnorm"
)

// Series is the consumer-facing view of one tracked series.
type Series struct {
	MetadataRef    refs.Metadata       `json:"metadataRef"`
	Ref            refs.Series         `json:"ref"`
	Root           string              `json:"root"`
	LastScanned    string              `json:"lastScanned,omitempty"`
	PreferredTitle textnorm.NFCString  `json:"preferredTitle"`
	CanonicalTitle *textnorm.NFCString `json:"canonicalTitle,omitempty"`
	Seasons        []Season            `json:"seasons"`
}

type Season struct {
	MetadataRef refs.Metadata `json:"metadataRef,omitempty"`
	Number      int           `json:"number"`
	Episodes    []Episode     `json:"episodes"`
}

type Episode struct {
	MetadataRef     refs.Metadata     `json:"metadataRef,omitempty"`
	Episode         refs.Episode      `json:"episode"`
	AbsoluteNumber  *int              `json:"absoluteNumber,omitempty"`
	Aired           string            `json:"aired,omitempty"`
	Status          EpisodeStatus     `json:"status"`
	Active          *EpisodeMedia     `json:"active,omitempty"`
	Staged          *EpisodeMedia     `json:"staged,omitempty"`
	Inconsistencies []FilesystemIssue `json:"inconsistencies,omitempty"`
}

type EpisodeMedia struct {
	Source     string          `json:"source"`
	Resolution string          `json:"resolution,omitempty"`
	Codec      string          `json:"codec,omitempty"`
	Size       int64           `json:"size"`
	File       string          `json:"file"`
	Companions []CompanionFile `json:"companions"`
}

type CompanionFile struct {
	Path     string `json:"path"`
	Role     string `json:"role,omitempty"`
	Language string `json:"language,omitempty"`
	Label    string `json:"label,omitempty"`
	Size     int64  `json:"size"`
	MTime    string `json:"mtime"`
}

type FilesystemIssue = layout.FilesystemIssue

type seriesState = state.State

type episodeState = state.Episode

type MediaRecord = state.MediaRecord

type CompanionRecord = state.CompanionRecord

func cloneMediaRecord(in MediaRecord) MediaRecord {
	return state.CloneMediaRecord(in)
}
