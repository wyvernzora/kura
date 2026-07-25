// Package seriesfile assigns the archive generation for each persisted series.
// A generation changes only when series identity, ordering, or active media
// facts that cannot be reconstructed from the files or metadata provider
// change. Provider-recoverable metadata, derived fields, and transient workflow
// state ride along without forcing a full-series archive rewrite.
package seriesfile

import (
	"bytes"
	"encoding/json"
)

type archiveView struct {
	MetadataRef string                  `json:"metadataRef"`
	Ordering    string                  `json:"ordering,omitempty"`
	Episodes    map[string]archiveMedia `json:"episodes"`
}

type archiveMedia struct {
	Path       string             `json:"path"`
	Source     string             `json:"source"`
	Size       int64              `json:"size"`
	MTime      string             `json:"mtime"`
	Companions []archiveCompanion `json:"companions"`
	Attrs      map[string]string  `json:"attrs,omitempty"`
}

type archiveCompanion struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	MTime string `json:"mtime"`
}

func archiveViewOf(in seriesV3) archiveView {
	episodes := make(map[string]archiveMedia)
	for ref, episode := range in.Episodes {
		if episode.Active == nil {
			continue
		}
		companions := make([]archiveCompanion, 0, len(episode.Active.Companions))
		for _, companion := range episode.Active.Companions {
			companions = append(companions, archiveCompanion{
				Path:  companion.Path,
				Size:  companion.Size,
				MTime: companion.MTime,
			})
		}
		episodes[ref] = archiveMedia{
			Path:       episode.Active.Path,
			Source:     episode.Active.Source,
			Size:       episode.Active.Size,
			MTime:      episode.Active.MTime,
			Companions: companions,
			Attrs:      episode.Active.Attrs,
		}
	}
	return archiveView{
		MetadataRef: in.MetadataRef,
		Ordering:    in.Ordering,
		Episodes:    episodes,
	}
}

func archiveEqual(a, b seriesV3) bool {
	aJSON, err := json.Marshal(archiveViewOf(a))
	if err != nil {
		panic(err)
	}
	bJSON, err := json.Marshal(archiveViewOf(b))
	if err != nil {
		panic(err)
	}
	return bytes.Equal(aJSON, bJSON)
}

func nextGeneration(prev, next seriesV3) int {
	if archiveEqual(prev, next) {
		return prev.Generation
	}
	return prev.Generation + 1
}
