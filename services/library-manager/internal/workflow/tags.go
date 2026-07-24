package workflow

import (
	"context"
	"slices"
	"strings"

	"github.com/wyvernzora/kura/services/library-manager/internal/coord"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/series"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/indexfile"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
)

type tagExpressions struct {
	positive []string
	negative []string
}

// UpdateTagsInput describes one atomic series-tag set mutation.
type UpdateTagsInput struct {
	Ref  refs.Series
	Tags []string
}

// UpdateTags applies plain expressions as additions and ! expressions as
// removals. Tags are opaque to Kura; only their syntax is validated.
func UpdateTags(ctx context.Context, deps Deps, in UpdateTagsInput) (api.SeriesTags, error) {
	exprs, err := parseTagExpressions(in.Tags, false)
	if err != nil {
		return api.SeriesTags{}, err
	}
	var out api.SeriesTags
	err = deps.Coordinator.WithSeries(ctx, in.Ref, func() error {
		return coord.RetryOnConflict(conflictAttempts(deps), func() error {
			model, loadErr := seriesfile.Load(deps.LibRoot, in.Ref)
			if loadErr != nil {
				return loadErr
			}
			if model.InProgress != nil {
				return &coord.BusyError{Scope: coord.SeriesScope(in.Ref), Holder: *model.InProgress}
			}
			changed := false
			for _, tag := range exprs.negative {
				changed = model.RemoveTag(tag) || changed
			}
			for _, tag := range exprs.positive {
				changed = model.AddTag(tag) || changed
			}
			if len(model.Tags) > series.MaxTags {
				return &InvalidTagError{Reason: "resulting tag count exceeds limit", Limit: series.MaxTags}
			}
			normalizedTags, normalizeErr := series.NormalizeTags(model.Tags)
			if normalizeErr != nil {
				return &InvalidTagError{Reason: normalizeErr.Error()}
			}
			model.Tags = normalizedTags
			tags := slices.Clone(model.Tags)
			if tags == nil {
				tags = []string{}
			}
			out = api.SeriesTags{MetadataRef: model.Metadata, Tags: tags}
			if !changed {
				return nil
			}
			if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("tag_update")); err != nil {
				return err
			}
			return updateIndexModel(ctx, deps, model, "tag_update")
		})
	})
	return out, err
}

func parseTagExpressions(raw []string, allowEmpty bool) (tagExpressions, error) {
	if len(raw) == 0 {
		if allowEmpty {
			return tagExpressions{}, nil
		}
		return tagExpressions{}, &InvalidTagError{Reason: "at least one tag expression is required"}
	}
	if len(raw) > series.MaxTags {
		return tagExpressions{}, &InvalidTagError{Reason: "tag expression count exceeds limit", Limit: series.MaxTags}
	}
	var out tagExpressions
	positive := map[string]struct{}{}
	negative := map[string]struct{}{}
	for _, expression := range raw {
		normalized := strings.ToLower(expression)
		remove := strings.HasPrefix(normalized, "!")
		tag := strings.TrimPrefix(normalized, "!")
		if err := series.ValidateTag(tag); err != nil {
			return tagExpressions{}, &InvalidTagError{Tag: expression, Reason: err.Error()}
		}
		if remove {
			if _, conflict := positive[tag]; conflict {
				return tagExpressions{}, &InvalidTagError{Tag: tag, Reason: "tag appears as both positive and negative"}
			}
			if _, exists := negative[tag]; !exists {
				negative[tag] = struct{}{}
				out.negative = append(out.negative, tag)
			}
			continue
		}
		if _, conflict := negative[tag]; conflict {
			return tagExpressions{}, &InvalidTagError{Tag: tag, Reason: "tag appears as both positive and negative"}
		}
		if _, exists := positive[tag]; !exists {
			positive[tag] = struct{}{}
			out.positive = append(out.positive, tag)
		}
	}
	return out, nil
}

func applyTagFilter(rows []indexfile.Row, filter tagExpressions) []indexfile.Row {
	if len(filter.positive) == 0 && len(filter.negative) == 0 {
		return rows
	}
	filtered := rows[:0]
	for _, row := range rows {
		if matchesTagFilter(row.Tags, filter) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func matchesTagFilter(tags []string, filter tagExpressions) bool {
	for _, required := range filter.positive {
		if !slices.Contains(tags, required) {
			return false
		}
	}
	for _, forbidden := range filter.negative {
		if slices.Contains(tags, forbidden) {
			return false
		}
	}
	return true
}
