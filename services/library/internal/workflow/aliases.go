package workflow

import (
	"context"
	"sort"
	"strings"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/internal/textnorm"
)

// ListAliases returns the persisted user aliases for the addressed
// series. TVDB-derived aliases are not surfaced — they're folded
// into searchKey at scan time and discarded.
func ListUserAliases(ctx context.Context, deps Deps, ref refs.Series) (response.UserAliasList, error) {
	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		return response.UserAliasList{}, err
	}
	return response.UserAliasList{Aliases: userAliasStrings(model.UserAliases)}, nil
}

// AddAliases appends each non-empty entry to the series's UserAliases
// (deduped), recomputes searchKey from persisted state, and persists
// via SaveCAS. Returns the resulting alias list. Idempotent: adding
// an existing alias is a no-op.
//
// Transient TVDB aliases are not in scope on this path — searchKey
// refolds against persisted titles + translations + the new user
// alias set. The next provider scan re-folds them in.
func AddUserAliases(ctx context.Context, deps Deps, ref refs.Series, aliases []string) (response.UserAliasList, error) {
	return mutateUserAliases(ctx, deps, ref, "alias_add", func(model interface {
		AddUserAlias(textnorm.NFCString) bool
	}, normalized []textnorm.NFCString) {
		for _, alias := range normalized {
			model.AddUserAlias(alias)
		}
	}, aliases)
}

// RemoveAliases drops each entry from the series's UserAliases,
// recomputes searchKey, and persists. Idempotent: removing a missing
// alias is a no-op.
func RemoveUserAliases(ctx context.Context, deps Deps, ref refs.Series, aliases []string) (response.UserAliasList, error) {
	return mutateUserAliases(ctx, deps, ref, "alias_rm", func(model interface {
		RemoveUserAlias(textnorm.NFCString) bool
	}, normalized []textnorm.NFCString) {
		for _, alias := range normalized {
			model.RemoveUserAlias(alias)
		}
	}, aliases)
}

// mutateUserAliases is the shared CAS-retried load/mutate/save body for
// add + remove. The mutate callback runs against the loaded model;
// callers receive normalized + deduped alias inputs.
func mutateUserAliases[T any](
	ctx context.Context,
	deps Deps,
	ref refs.Series,
	op string,
	apply func(model T, normalized []textnorm.NFCString),
	rawInputs []string,
) (response.UserAliasList, error) {
	normalized := normalizeUserAliasInputs(rawInputs)
	var out response.UserAliasList
	err := deps.Coordinator.WithSeries(ctx, ref, func() error {
		return coord.RetryOnConflict(coord.AttemptsFromEnv(), func() error {
			model, err := seriesfile.Load(deps.LibRoot, ref)
			if err != nil {
				return err
			}
			if model.InProgress != nil {
				return &coord.BusyError{Scope: coord.SeriesScope(ref), Holder: *model.InProgress}
			}
			// Type-assert into the apply callback's interface so AddAliases
			// + RemoveAliases share this body. The compiler enforces that
			// `model` (always *series.Series here) satisfies T.
			apply(any(model).(T), normalized)
			model.RecomputeSearchKey(deps.PreferredLanguages, nil, nil)
			if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator(op)); err != nil {
				return err
			}
			if err := updateIndexRow(ctx, deps, model, op); err != nil {
				return err
			}
			out = response.UserAliasList{Aliases: userAliasStrings(model.UserAliases)}
			return nil
		})
	})
	return out, err
}

// normalizeUserAliasInputs trims whitespace, NFC-normalizes, and dedupes
// (preserving first-seen order). Empty entries are dropped.
func normalizeUserAliasInputs(in []string) []textnorm.NFCString {
	if len(in) == 0 {
		return nil
	}
	out := make([]textnorm.NFCString, 0, len(in))
	seen := map[string]struct{}{}
	for _, raw := range in {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		nfc := textnorm.NFC(raw)
		key := nfc.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, nfc)
	}
	return out
}

// userAliasStrings flattens persisted NFC aliases into the wire shape.
// Sorted alphabetically for stable diffs against the on-disk file +
// predictable response ordering.
func userAliasStrings(in []textnorm.NFCString) []string {
	out := make([]string, 0, len(in))
	for _, alias := range in {
		out = append(out, alias.String())
	}
	sort.Strings(out)
	return out
}
