package reconcile

import (
	"crypto/sha256"
	"encoding/base32"
	"strconv"
)

// stepIDHashBytes is the truncation length of the sha256 digest used
// for step IDs. 10 bytes (80 bits) is plenty of collision resistance
// for the few-hundred-step plans we generate; base32-encoding 10 bytes
// yields 16 chars (no padding required at this length).
const stepIDHashBytes = 10

var stepIDEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// DeriveStepID returns the deterministic 16-char base32 step ID for a
// step with the given attributes. The ID is a function of the plan
// token plus all step-identifying fields, so:
//
//   - Same series state → same token → same step IDs (replanning the
//     same state produces byte-identical plan content).
//   - Different attributes → different IDs (sha256 collision is not a
//     concern at plan size).
//
// Mirrors the cursor encoding scheme from internal/workflow/list.go:
// sha256 + truncate + base32 RFC 4648 with no padding.
func DeriveStepID(token string, owner Owner, kind StepKind, from, to, path string) string {
	h := sha256.New()
	h.Write([]byte(token))
	h.Write([]byte{0})
	h.Write([]byte(owner.Kind))
	h.Write([]byte{0})
	h.Write([]byte(ownerStableKey(owner)))
	h.Write([]byte{0})
	h.Write([]byte(kind))
	h.Write([]byte{0})
	h.Write([]byte(from))
	h.Write([]byte{0})
	h.Write([]byte(to))
	h.Write([]byte{0})
	h.Write([]byte(path))
	sum := h.Sum(nil)
	return stepIDEncoding.EncodeToString(sum[:stepIDHashBytes])
}

// ownerStableKey returns the stable identifier within an owner kind:
//   - episode  → episode ref (storage form)
//   - trash    → trash ULID (string form). For replaced-active steps
//     the OriginalEpisode is folded in too so the same trash
//     target reached from different episodes (impossible in
//     practice but covered here) wouldn't collide.
//   - extra    → extra ULID + season + prefix.
func ownerStableKey(o Owner) string {
	switch o.Kind {
	case OwnerEpisode:
		return o.EpisodeRef.String()
	case OwnerTrash:
		key := o.TrashID
		if !o.OriginalEpisode.IsZero() {
			key += "|" + o.OriginalEpisode.String()
		}
		return key
	case OwnerExtra:
		return o.ExtraID + "|" + strconv.Itoa(o.Season) + "|" + o.Prefix
	default:
		return ""
	}
}
