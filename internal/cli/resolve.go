package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/wyvernzora/kura/internal/cli/prompt"
	"github.com/wyvernzora/kura/internal/cli/stdio"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/workflow"
)

// ErrAmbiguousSelector is returned when WithResolve is asked to pick
// among multiple candidates without a TTY available for the prompt.
// Surfaces translate to "specify a metadata ref" guidance.
var ErrAmbiguousSelector = errors.New("cli: selector matched multiple candidates and stdin is not a terminal")

// ErrNoMetadataMatch is returned when WithResolve gets back zero
// candidates from workflow.Resolve.
var ErrNoMetadataMatch = errors.New("cli: no metadata match for selector")

// WithResolve composes workflow.Resolve with the action a CLI command
// wants to run on the resolved metadata ref. The composition rule:
//
//   - len(Candidates) == 0 → return "no metadata match" error.
//   - len(Candidates) == 1 → run action with that candidate's ref.
//   - len(Candidates) >  1 + interactive → prompt; run action with pick.
//   - len(Candidates) >  1 + non-interactive → return ambiguity error.
//
// Workflows are kept surface-agnostic by keeping this composition out of
// internal/workflow/. MCP exposes Resolve and action workflows
// separately; agents handle ambiguity themselves.
func WithResolve(
	ctx context.Context,
	io stdio.Stdio,
	deps workflow.Deps,
	terms []string,
	action func(refs.Metadata) error,
) error {
	resolution, err := workflow.Resolve(ctx, deps, workflow.ResolveInput{Terms: terms})
	if err != nil {
		return err
	}
	switch len(resolution.Candidates) {
	case 0:
		return fmt.Errorf("%w: %v", ErrNoMetadataMatch, terms)
	case 1:
		return action(resolution.Candidates[0].Ref)
	}
	picked, err := prompt.SelectCandidate(io, terms, resolution.Candidates)
	if err != nil {
		if errors.Is(err, prompt.ErrNonInteractive) {
			return fmt.Errorf("%w: selector %v matched %d candidates; pass a metadata ref directly to disambiguate", ErrAmbiguousSelector, terms, len(resolution.Candidates))
		}
		return err
	}
	return action(picked.Ref)
}
