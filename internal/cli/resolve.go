package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/wyvernzora/kura/internal/cli/prompt"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/ui/stdio"
	"github.com/wyvernzora/kura/internal/workflow"
)

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
		return fmt.Errorf("no metadata match for %v", terms)
	case 1:
		return action(resolution.Candidates[0].Ref)
	}
	picked, err := prompt.SelectCandidate(io, terms, resolution.Candidates)
	if err != nil {
		if errors.Is(err, prompt.ErrNonInteractive) {
			return fmt.Errorf("selector %v matched %d candidates; pass a metadata ref directly to disambiguate", terms, len(resolution.Candidates))
		}
		return err
	}
	return action(picked.Ref)
}
