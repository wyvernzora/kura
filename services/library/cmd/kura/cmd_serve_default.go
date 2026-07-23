//go:build !e2e_stub

package main

import "github.com/wyvernzora/kura/services/library/internal/workflow"

// applyTestStubs is a no-op in production builds. The serveCmd flags
// --provider-stub / --inspector-stub are hidden and silently ignored
// here; build with -tags=e2e_stub to enable them.
func applyTestStubs(deps workflow.Deps, _ *serveCmd) (workflow.Deps, error) {
	return deps, nil
}
