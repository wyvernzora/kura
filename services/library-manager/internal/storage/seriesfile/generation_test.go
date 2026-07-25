package seriesfile

import (
	"reflect"
	"testing"
)

func TestArchiveViewClassifiesEveryField(t *testing.T) {
	archiveRelevant := map[string]struct{}{
		"seriesV3.MetadataRef":     {},
		"seriesV3.Ordering":        {},
		"seriesV3.Episodes":        {},
		"episodeV2.Active":         {},
		"mediaRecordV1.Path":       {},
		"mediaRecordV1.Source":     {},
		"mediaRecordV1.Size":       {},
		"mediaRecordV1.MTime":      {},
		"mediaRecordV1.Companions": {},
		"mediaRecordV1.Attrs":      {},
		"companionRecordV1.Path":   {},
		"companionRecordV1.Size":   {},
		"companionRecordV1.MTime":  {},
	}
	churn := map[string]struct{}{
		"seriesV3.SchemaVersion":     {},
		"seriesV3.Generation":        {},
		"seriesV3.PreferredTitle":    {},
		"seriesV3.CanonicalTitle":    {},
		"seriesV3.DateAdded":         {},
		"seriesV3.LastScanned":       {},
		"seriesV3.Artwork":           {},
		"seriesV3.StagedTrash":       {},
		"seriesV3.StagedExtras":      {},
		"seriesV3.UserAliases":       {},
		"seriesV3.Tags":              {},
		"seriesV3.SearchKey":         {},
		"seriesV3.InProgress":        {},
		"seriesV3.LastMutated":       {},
		"episodeV2.AirDate":          {},
		"episodeV2.PreferredTitle":   {},
		"episodeV2.CanonicalTitle":   {},
		"episodeV2.Staged":           {},
		"mediaRecordV1.Resolution":   {},
		"mediaRecordV1.Codec":        {},
		"companionRecordV1.Role":     {},
		"companionRecordV1.Language": {},
		"companionRecordV1.Label":    {},
	}
	types := []reflect.Type{
		reflect.TypeFor[seriesV3](),
		reflect.TypeFor[episodeV2](),
		reflect.TypeFor[mediaRecordV1](),
		reflect.TypeFor[companionRecordV1](),
	}
	seen := make(map[string]struct{})
	for _, typ := range types {
		for field := range typ.Fields() {
			name := typ.Name() + "." + field.Name
			_, relevant := archiveRelevant[name]
			_, ignored := churn[name]
			if relevant == ignored {
				t.Fatalf(
					"%s classification: archive-relevant=%t churn=%t; want exactly one",
					name,
					relevant,
					ignored,
				)
			}
			seen[name] = struct{}{}
		}
	}
	for name := range archiveRelevant {
		if _, ok := seen[name]; !ok {
			t.Fatalf("archive-relevant classification names missing field %s", name)
		}
	}
	for name := range churn {
		if _, ok := seen[name]; !ok {
			t.Fatalf("churn classification names missing field %s", name)
		}
	}
}
