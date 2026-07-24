//go:build e2e_stub

package main

import (
	"github.com/wyvernzora/kura/services/library-manager/internal/provider"
	"github.com/wyvernzora/kura/services/library-manager/internal/teststub"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
)

// applyTestStubs swaps the provider and inspector for in-memory
// fixtures when --use-test-stubs is set. Compiled only under the
// e2e_stub build tag; production binary uses the no-op variant in
// stubs_default.go.
//
// --stub-provider-fixture=PATH overrides the provider fixture path;
// empty path falls back to the default in-process fixture
// (teststub.NewDefaultProvider).
func applyTestStubs(deps workflow.Deps, opts serverOptions) (workflow.Deps, error) {
	if !opts.UseTestStubs {
		return deps, nil
	}
	prov, err := teststub.LoadProvider(opts.StubProviderFixture)
	if err != nil {
		return workflow.Deps{}, err
	}
	deps.Provider = workflow.NewProviderFactory(func() (provider.Source, error) {
		return prov, nil
	})
	deps.Inspector = teststub.NewDefaultInspector()
	return deps, nil
}
