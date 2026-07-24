//go:build !e2e_stub

package main

import "github.com/wyvernzora/kura/services/library-manager/internal/workflow"

// applyTestStubs is a no-op in production builds. The test-only flags
// --use-test-stubs / --stub-provider-fixture are silently ignored here;
// build with -tags=e2e_stub to enable them.
func applyTestStubs(deps workflow.Deps, _ serverOptions) (workflow.Deps, error) {
	return deps, nil
}
