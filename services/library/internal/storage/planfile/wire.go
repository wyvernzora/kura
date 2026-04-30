package planfile

import (
	"fmt"
	"time"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/reconcile"
)

// Plan v2 — JSONL with type-discriminated lines:
//
//   - Line 1: header (type:"header") — token, lifetime, series, snapshot.
//   - Lines 2..N+1: steps (type:"step") — fully-unrolled file_move /
//     dir_remove operations with deterministic IDs and owner metadata.
//   - Lines N+2..M: events (type:"event") + result (type:"result"),
//     appended by apply.
//
// Reader streams by type discriminator. Plan content ends implicitly
// when the first non-step line appears.

type headerV2 struct {
	Type          string `json:"type"` // "header"
	SchemaVersion int    `json:"schemaVersion"`
	Token         string `json:"token"`
	CreatedAt     string `json:"createdAt"`
	ExpiresAt     string `json:"expiresAt"`
	Series        string `json:"series"`
	Snapshot      string `json:"snapshot"`
}

type stepV2 struct {
	Type  string  `json:"type"` // "step"
	ID    string  `json:"id"`
	Kind  string  `json:"kind"`
	Owner ownerV2 `json:"owner"`
	From  string  `json:"from,omitempty"`
	To    string  `json:"to,omitempty"`
	Path  string  `json:"path,omitempty"`
}

type ownerV2 struct {
	Kind            string            `json:"kind"`
	EpisodeRef      string            `json:"episodeRef,omitempty"`
	EpisodeIntent   string            `json:"episodeIntent,omitempty"`
	TrashID         string            `json:"trashId,omitempty"`
	OriginalEpisode string            `json:"originalEpisode,omitempty"`
	Record          *replacedRecordV2 `json:"record,omitempty"`
	StagedRecord    *replacedRecordV2 `json:"stagedRecord,omitempty"`
	ExtraID         string            `json:"extraId,omitempty"`
	Season          int               `json:"season,omitempty"`
	Prefix          string            `json:"prefix,omitempty"`
}

type replacedRecordV2 struct {
	Path       string                `json:"path"`
	Source     string                `json:"source,omitempty"`
	Resolution string                `json:"resolution,omitempty"`
	Codec      string                `json:"codec,omitempty"`
	Size       int64                 `json:"size"`
	MTime      string                `json:"mtime"`
	Companions []replacedCompanionV2 `json:"companions,omitempty"`
}

type replacedCompanionV2 struct {
	Path     string `json:"path"`
	Role     string `json:"role,omitempty"`
	Language string `json:"language,omitempty"`
	Label    string `json:"label,omitempty"`
	Size     int64  `json:"size"`
	MTime    string `json:"mtime"`
}

type eventV2 struct {
	Type  string `json:"type"` // "event"
	At    string `json:"at"`
	Step  string `json:"step"`
	Error string `json:"error,omitempty"`
}

type resultV2 struct {
	Type         string `json:"type"` // "result"
	At           string `json:"at"`
	Status       string `json:"status"`
	AppliedSteps int    `json:"appliedSteps,omitempty"`
	Error        string `json:"error,omitempty"`
}

func headerToWire(h reconcile.Header) headerV2 {
	return headerV2{
		Type:          "header",
		SchemaVersion: currentSchemaVersion,
		Token:         h.Token,
		CreatedAt:     h.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt:     h.ExpiresAt.UTC().Format(time.RFC3339),
		Series:        h.Series.String(),
		Snapshot:      h.Snapshot,
	}
}

func headerFromWire(in headerV2) (reconcile.Header, error) {
	if in.Type != "header" {
		return reconcile.Header{}, fmt.Errorf("planfile: line 1 type %q, want \"header\"", in.Type)
	}
	if in.SchemaVersion != currentSchemaVersion {
		return reconcile.Header{}, fmt.Errorf("planfile: unsupported schemaVersion %d (want %d); re-plan", in.SchemaVersion, currentSchemaVersion)
	}
	createdAt, err := time.Parse(time.RFC3339, in.CreatedAt)
	if err != nil {
		return reconcile.Header{}, fmt.Errorf("planfile: invalid createdAt %q: %w", in.CreatedAt, err)
	}
	expiresAt, err := time.Parse(time.RFC3339, in.ExpiresAt)
	if err != nil {
		return reconcile.Header{}, fmt.Errorf("planfile: invalid expiresAt %q: %w", in.ExpiresAt, err)
	}
	seriesRef, err := refs.ParseSeries(in.Series)
	if err != nil {
		return reconcile.Header{}, err
	}
	return reconcile.Header{
		SchemaVersion: in.SchemaVersion,
		Token:         in.Token,
		CreatedAt:     createdAt,
		ExpiresAt:     expiresAt,
		Series:        seriesRef,
		Snapshot:      in.Snapshot,
	}, nil
}

func stepToWire(s reconcile.Step) stepV2 {
	return stepV2{
		Type:  "step",
		ID:    s.ID,
		Kind:  string(s.Kind),
		Owner: ownerToWire(s.Owner),
		From:  s.From,
		To:    s.To,
		Path:  s.Path,
	}
}

func stepFromWire(in stepV2) (reconcile.Step, error) {
	owner, err := ownerFromWire(in.Owner)
	if err != nil {
		return reconcile.Step{}, err
	}
	return reconcile.Step{
		ID:    in.ID,
		Kind:  reconcile.StepKind(in.Kind),
		Owner: owner,
		From:  in.From,
		To:    in.To,
		Path:  in.Path,
	}, nil
}

func ownerToWire(o reconcile.Owner) ownerV2 {
	out := ownerV2{
		Kind:          string(o.Kind),
		EpisodeIntent: o.EpisodeIntent,
		TrashID:       o.TrashID,
		ExtraID:       o.ExtraID,
		Season:        o.Season,
		Prefix:        o.Prefix,
	}
	if !o.EpisodeRef.IsZero() {
		out.EpisodeRef = o.EpisodeRef.String()
	}
	if !o.OriginalEpisode.IsZero() {
		out.OriginalEpisode = o.OriginalEpisode.String()
	}
	if o.Record != nil {
		w := replacedRecordToWire(*o.Record)
		out.Record = &w
	}
	if o.StagedRecord != nil {
		w := replacedRecordToWire(*o.StagedRecord)
		out.StagedRecord = &w
	}
	return out
}

func ownerFromWire(in ownerV2) (reconcile.Owner, error) {
	out := reconcile.Owner{
		Kind:          reconcile.OwnerKind(in.Kind),
		EpisodeIntent: in.EpisodeIntent,
		TrashID:       in.TrashID,
		ExtraID:       in.ExtraID,
		Season:        in.Season,
		Prefix:        in.Prefix,
	}
	if in.EpisodeRef != "" {
		ep, err := refs.ParseEpisode(in.EpisodeRef)
		if err != nil {
			return reconcile.Owner{}, fmt.Errorf("planfile: owner.episodeRef %q: %w", in.EpisodeRef, err)
		}
		out.EpisodeRef = ep
	}
	if in.OriginalEpisode != "" {
		ep, err := refs.ParseEpisode(in.OriginalEpisode)
		if err != nil {
			return reconcile.Owner{}, fmt.Errorf("planfile: owner.originalEpisode %q: %w", in.OriginalEpisode, err)
		}
		out.OriginalEpisode = ep
	}
	if in.Record != nil {
		rec, err := replacedRecordFromWire(*in.Record)
		if err != nil {
			return reconcile.Owner{}, err
		}
		out.Record = &rec
	}
	if in.StagedRecord != nil {
		rec, err := replacedRecordFromWire(*in.StagedRecord)
		if err != nil {
			return reconcile.Owner{}, err
		}
		out.StagedRecord = &rec
	}
	return out, nil
}

func replacedRecordToWire(r reconcile.ReplacedRecord) replacedRecordV2 {
	out := replacedRecordV2{
		Path:       r.Path,
		Source:     r.Source,
		Resolution: r.Resolution,
		Codec:      r.Codec,
		Size:       r.Size,
		MTime:      r.MTime.UTC().Format(time.RFC3339),
		Companions: make([]replacedCompanionV2, 0, len(r.Companions)),
	}
	for _, c := range r.Companions {
		out.Companions = append(out.Companions, replacedCompanionV2{
			Path:     c.Path,
			Role:     c.Role,
			Language: c.Language,
			Label:    c.Label,
			Size:     c.Size,
			MTime:    c.MTime.UTC().Format(time.RFC3339),
		})
	}
	return out
}

func replacedRecordFromWire(in replacedRecordV2) (reconcile.ReplacedRecord, error) {
	mtime, err := time.Parse(time.RFC3339, in.MTime)
	if err != nil {
		return reconcile.ReplacedRecord{}, fmt.Errorf("planfile: record.mtime %q: %w", in.MTime, err)
	}
	out := reconcile.ReplacedRecord{
		Path:       in.Path,
		Source:     in.Source,
		Resolution: in.Resolution,
		Codec:      in.Codec,
		Size:       in.Size,
		MTime:      mtime,
		Companions: make([]reconcile.ReplacedCompanion, 0, len(in.Companions)),
	}
	for _, c := range in.Companions {
		cmt, err := time.Parse(time.RFC3339, c.MTime)
		if err != nil {
			return reconcile.ReplacedRecord{}, fmt.Errorf("planfile: companion.mtime %q: %w", c.MTime, err)
		}
		out.Companions = append(out.Companions, reconcile.ReplacedCompanion{
			Path:     c.Path,
			Role:     c.Role,
			Language: c.Language,
			Label:    c.Label,
			Size:     c.Size,
			MTime:    cmt,
		})
	}
	return out, nil
}
