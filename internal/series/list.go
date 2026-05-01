package series

import (
	"time"

	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/textnorm"
)

type ListSummary struct {
	MetadataRef    refs.Metadata      `json:"metadataRef,omitempty"`
	PreferredTitle textnorm.NFCString `json:"preferredTitle"`
	CanonicalTitle textnorm.NFCString `json:"canonicalTitle"`
	SeasonCount    int                `json:"seasonCount"`
	EpisodeCount   int                `json:"episodeCount"`
	MissingCount   int                `json:"missingCount"`
	PendingCount   int                `json:"pendingCount"`
	HasStaged      bool               `json:"hasStaged"`
}

func ReadListSummary(root string, ref refs.Series, now time.Time) (ListSummary, error) {
	model, err := repo{root: root}.load(ref)
	if err != nil {
		return ListSummary{}, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	title := model.PreferredTitle
	if title.IsZero() {
		title = textnorm.NFC(ref.String())
	}
	summary := ListSummary{
		MetadataRef:    model.Metadata,
		PreferredTitle: title,
		CanonicalTitle: model.CanonicalTitle,
	}
	seasons := map[int]struct{}{}
	for episodeRef, episode := range model.Episodes {
		if episode.Staged != nil {
			summary.HasStaged = true
		}
		if episodeRef.IsSpecial() {
			continue
		}
		summary.EpisodeCount++
		seasons[episodeRef.Season()] = struct{}{}
		if episode.Active != nil || episode.Staged != nil {
			continue
		}
		if isPendingEpisode(episode.AirDate, now) {
			summary.PendingCount++
			continue
		}
		summary.MissingCount++
	}
	summary.SeasonCount = len(seasons)
	return summary, nil
}
