// Package series defines the in-memory shape of a tracked series and the
// pure operations on it. No IO. Translation between this shape and the
// on-disk wire format lives in storage/seriesfile.
package series

import (
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/civil"
	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/provider"
	"github.com/wyvernzora/kura/internal/searchkey"
	"github.com/wyvernzora/kura/internal/textnorm"
)

// ParseAirDate parses YYYY-MM-DD or empty into a civil.Date. Empty input
// returns the zero value (civil.Date.IsValid() reports false).
func ParseAirDate(value string) (civil.Date, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return civil.Date{}, nil
	}
	return civil.ParseDate(value)
}

// Series is the persisted shape of a tracked series in memory. It is
// round-trippable through seriesfile.Load/Save.
//
// Ref is the series filesystem ref the loader read this Series from. It is
// not part of the wire format; seriesfile.Load populates it after successful
// decode so call sites do not need to track the ref alongside *Series.
//
// InProgress is set when a claim-holding workflow (currently only
// reconcile apply) is mid-flight against this series. Claim-respecting
// workflows refuse to mutate when InProgress is non-nil and not stale.
//
// LastMutated is stamped by every successful CAS write for diagnostics;
// surfaces include the winning side's identity in ConflictError messages.
//
// Hash is the SHA-256 of the file bytes this Series was loaded from.
// seriesfile.Load populates it; seriesfile.SaveCAS uses it as the
// expected on-disk hash for the optimistic check. Empty means "no prior
// load; create new file via O_EXCL".
type Series struct {
	Ref            refs.Series
	Metadata       refs.Metadata
	PreferredTitle textnorm.NFCString
	CanonicalTitle textnorm.NFCString
	LastScanned    time.Time
	Ordering       string
	Episodes       map[refs.Episode]Episode
	// StagedTrash carries files queued for trash on the next reconcile.
	// Always a non-nil slice (decode initializes empty); empty means "no
	// trash queued".
	StagedTrash []StagedTrashItem
	// StagedExtras carries extras (BTS, OVAs, etc.) queued for placement
	// under Season N/Extra/[prefix]/ on the next reconcile. Always
	// non-nil (decode initializes empty).
	StagedExtras []StagedExtraItem
	Artwork      Artwork
	// UserAliases are hand-coined shorthands managed via the alias
	// REST endpoints / `kura alias` CLI. Preserved across rescans;
	// rescan-time TVDB aliases + translated titles land separately
	// (transient, folded straight into SearchKey and discarded).
	UserAliases []textnorm.NFCString
	// SearchKey is the persisted output of `internal/searchkey.Compute`.
	// The only search-related field we persist — provider aliases +
	// translated titles flow in transiently per scan / add and never
	// land on disk. Empty when no candidate produces a token.
	SearchKey   string
	InProgress  *coord.Holder
	LastMutated coord.Mutator
	Hash        string
}

// StagedTrashItem represents one file (and its companions) queued for
// trash on the next reconcile_apply. ID is the ULID assigned at stage
// time and reused as the trash bucket directory name. Path is
// series-root-relative slash form on disk; absolutized on Load and
// relativized on Save (mirrors active record handling).
type StagedTrashItem struct {
	ID         ulid.ULID
	Path       string
	Size       int64
	MTime      time.Time
	AddedAt    time.Time
	Companions []media.Companion
}

// StagedExtraItem represents one extra (file or directory) queued for
// placement under Season N/Extra/[Prefix]/<basename> on the next
// reconcile_apply. Path is stored as an inbox: selector verbatim.
type StagedExtraItem struct {
	ID      ulid.ULID
	Season  int
	Path    string
	Prefix  string
	IsDir   bool
	AddedAt time.Time
}

// Episode is the persisted shape for one episode slot. PreferredTitle
// follows the operator's KURA_PREFERRED_LANGUAGES order; falls back to
// CanonicalTitle when no provider translation exists. CanonicalTitle is
// the provider's default-language episode name.
type Episode struct {
	AirDate        civil.Date
	PreferredTitle textnorm.NFCString
	CanonicalTitle textnorm.NFCString
	Active         *media.Record
	Staged         *media.Record
}

// SpineEntry describes one episode slot in the metadata-derived spine. Used
// to seed or refresh a Series's Episodes. PreferredTitle and CanonicalTitle
// ride alongside the spine when scan refreshes from the provider; both
// are empty for legacy callers that don't surface titles.
type SpineEntry struct {
	Ref            refs.Episode
	AirDate        civil.Date
	PreferredTitle textnorm.NFCString
	CanonicalTitle textnorm.NFCString
}

// Artwork bundles every series-level image surface kura persists. v1
// of the field carries Poster only; banner / fanart / clearlogo can be
// added as additional fields without reshuffling the wire schema or
// the convert layer.
type Artwork struct {
	Poster Poster
}

// IsZero reports whether no artwork is populated.
func (a Artwork) IsZero() bool { return a.Poster.IsZero() }

// Poster is a series-level artwork URL pulled from the metadata
// provider. URLs only — kura does not cache image bytes locally.
// Language is the BCP-47 code reported by the provider; empty means
// "default / not language-tagged."
type Poster struct {
	URL          string
	ThumbnailURL string
	Language     string
}

// IsZero reports whether the poster carries no URL.
func (p Poster) IsZero() bool { return p.URL == "" }

// RefreshSpine adds new spine entries and updates known air dates,
// preferred + canonical titles without removing any existing episodes.
// Title fields are overwritten with whatever the spine entry carries —
// the spine is the provider's view, so an empty incoming title means
// the provider has no title for that slot at this scan.
func (s *Series) RefreshSpine(spine []SpineEntry) {
	if s.Episodes == nil {
		s.Episodes = map[refs.Episode]Episode{}
	}
	for _, incoming := range spine {
		episode := s.Episodes[incoming.Ref]
		episode.AirDate = incoming.AirDate
		episode.PreferredTitle = incoming.PreferredTitle
		episode.CanonicalTitle = incoming.CanonicalTitle
		s.Episodes[incoming.Ref] = episode
	}
}

// PruneSpine removes empty orphan slots that the provider no longer
// knows about (i.e. refs absent from known). Slots with active or
// staged records are kept and returned in the orphan slice so callers
// can surface the conflict.
func (s *Series) PruneSpine(known map[refs.Episode]struct{}) []refs.Episode {
	var orphans []refs.Episode
	for ref, episode := range s.Episodes {
		if _, ok := known[ref]; ok {
			continue
		}
		if episode.Active != nil || episode.Staged != nil {
			orphans = append(orphans, ref)
			continue
		}
		delete(s.Episodes, ref)
	}
	return orphans
}

func (s *Series) SetStaged(ref refs.Episode, record media.Record) error {
	episode, ok := s.Episodes[ref]
	if !ok {
		return fmt.Errorf("series: metadata has no %s", ref)
	}
	record.Companions = append([]media.Companion(nil), record.Companions...)
	if record.Companions == nil {
		record.Companions = []media.Companion{}
	}
	episode.Staged = &record
	s.Episodes[ref] = episode
	return nil
}

func (s *Series) ClearStaged(ref refs.Episode) error {
	episode, ok := s.Episodes[ref]
	if !ok {
		return fmt.Errorf("series: metadata has no %s", ref)
	}
	episode.Staged = nil
	s.Episodes[ref] = episode
	return nil
}

// PromoteStaged moves the staged record into the active slot and clears
// staged. Returns the previous active record (if any) so callers can track it
// for trash.
func (s *Series) PromoteStaged(ref refs.Episode) (*media.Record, error) {
	episode, ok := s.Episodes[ref]
	if !ok {
		return nil, fmt.Errorf("series: metadata has no %s", ref)
	}
	if episode.Staged == nil {
		return nil, fmt.Errorf("series: %s has no staged media", ref)
	}
	var replaced *media.Record
	if episode.Active != nil {
		record := media.CloneRecord(*episode.Active)
		replaced = &record
	}
	active := media.CloneRecord(*episode.Staged)
	episode.Active = &active
	episode.Staged = nil
	s.Episodes[ref] = episode
	return replaced, nil
}

func (s *Series) SetActive(ref refs.Episode, record media.Record) error {
	episode, ok := s.Episodes[ref]
	if !ok {
		return fmt.Errorf("series: metadata has no %s", ref)
	}
	record.Companions = append([]media.Companion(nil), record.Companions...)
	if record.Companions == nil {
		record.Companions = []media.Companion{}
	}
	episode.Active = &record
	s.Episodes[ref] = episode
	return nil
}

func (s *Series) ClearActive(ref refs.Episode) error {
	episode, ok := s.Episodes[ref]
	if !ok {
		return fmt.Errorf("series: metadata has no %s", ref)
	}
	episode.Active = nil
	s.Episodes[ref] = episode
	return nil
}

// AddStagedTrash appends item to s.StagedTrash. Caller is responsible
// for ULID uniqueness (workflow assigns at stage time).
func (s *Series) AddStagedTrash(item StagedTrashItem) {
	s.StagedTrash = append(s.StagedTrash, item)
}

// RemoveStagedTrash drops the entry whose ID matches id and returns
// true. Returns false (no-op) if no entry matches.
func (s *Series) RemoveStagedTrash(id ulid.ULID) bool {
	for i := range s.StagedTrash {
		if s.StagedTrash[i].ID == id {
			s.StagedTrash = append(s.StagedTrash[:i], s.StagedTrash[i+1:]...)
			return true
		}
	}
	return false
}

// ClearStagedTrash drops every staged-trash entry. Used by reconcile
// apply post-success to mark all queued items as consumed.
func (s *Series) ClearStagedTrash() {
	s.StagedTrash = s.StagedTrash[:0]
}

// AddStagedExtra appends item to s.StagedExtras.
func (s *Series) AddStagedExtra(item StagedExtraItem) {
	s.StagedExtras = append(s.StagedExtras, item)
}

// RemoveStagedExtra drops the entry whose ID matches id and returns
// true. Returns false (no-op) if no entry matches.
func (s *Series) RemoveStagedExtra(id ulid.ULID) bool {
	for i := range s.StagedExtras {
		if s.StagedExtras[i].ID == id {
			s.StagedExtras = append(s.StagedExtras[:i], s.StagedExtras[i+1:]...)
			return true
		}
	}
	return false
}

// ClearStagedExtras drops every staged-extra entry. Used by reconcile
// apply post-success.
func (s *Series) ClearStagedExtras() {
	s.StagedExtras = s.StagedExtras[:0]
}

// AddUserAlias appends alias to UserAliases if not already present
// (case-sensitive against the NFC form). Empty / whitespace-only
// values are ignored. Returns true when the slice grew.
func (s *Series) AddUserAlias(alias textnorm.NFCString) bool {
	if alias.IsZero() || strings.TrimSpace(alias.String()) == "" {
		return false
	}
	for _, existing := range s.UserAliases {
		if existing == alias {
			return false
		}
	}
	s.UserAliases = append(s.UserAliases, alias)
	return true
}

// RemoveUserAlias drops the entry equal to alias. Returns true when
// the slice shrunk.
func (s *Series) RemoveUserAlias(alias textnorm.NFCString) bool {
	for i, existing := range s.UserAliases {
		if existing == alias {
			s.UserAliases = append(s.UserAliases[:i], s.UserAliases[i+1:]...)
			return true
		}
	}
	return false
}

// RecomputeSearchKey rebuilds SearchKey from the in-memory state
// (titles + UserAliases) plus any transient TVDB aliases / translated
// titles the caller has on hand from the provider response. The
// Add / scan paths pass both transient slices; the CLI alias-mutate
// path passes nil for both — TVDB material lands back on the next
// scan.
//
// `prefs` is the user's KURA_PREFERRED_LANGUAGES (BCP-47 base form).
// Empty list disables the translation channel.
func (s *Series) RecomputeSearchKey(
	prefs []string,
	transientAliases []provider.TitleEntry,
	transientTranslated []provider.TitleEntry,
) {
	tt := make([]searchkey.TranslatedTitle, 0, len(transientTranslated))
	for _, entry := range transientTranslated {
		tt = append(tt, searchkey.TranslatedTitle{
			Language: entry.Language,
			Value:    entry.Value,
		})
	}
	users := make([]string, 0, len(s.UserAliases))
	for _, alias := range s.UserAliases {
		users = append(users, alias.String())
	}
	s.SearchKey = searchkey.Compute(searchkey.Inputs{
		Canonical:        s.CanonicalTitle.String(),
		Preferred:        s.PreferredTitle.String(),
		TranslatedTitles: tt,
		Aliases:          transientAliases,
		UserAliases:      users,
		PreferredLangs:   prefs,
	})
}
