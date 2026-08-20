# Kura monorepo — root orchestration. Each module owns its Makefile;
# these targets fan out.

MODULES := services/library-manager services/release-indexer services/gateway services/gateway/web cli integrations/n8n

# Go modules in the workspace, checked standalone: the workspace resolves
# dependencies the individual modules cannot, so a module can build here and
# fail in CI, which runs each with GOWORK=off.
GO_MODULES := services/library-manager services/release-indexer services/gateway cli

.PHONY: check work-sync build test service-e2e product-e2e e2e

# Workspace-level invariant, owned by no module: a dependency added in one
# module leaves the other modules' go.mod/go.sum stale until `go work sync`
# runs. Per-module `make check` cannot see it — the drift is between modules
# — so it has to live at the root. Mirrors ci-releases' "go work sync drift"
# step, which is where this last escaped to. Two distinct failure modes live
# here: go.mod/go.sum drift between modules, and a sync-promoted version whose
# /go.mod hash never landed in a module's standalone go.sum — the second only
# shows up with GOWORK=off, which is how CI builds each module.
work-sync:
	@go work sync
	@git diff --exit-code -- '*/go.mod' '*/go.sum' '**/go.mod' '**/go.sum' \
	  || (echo "go.mod/go.sum drift: 'go work sync' changed files — review and commit them" >&2 && exit 1)
	@for m in $(GO_MODULES); do \
		(cd $$m && GOWORK=off go build ./... >/dev/null) \
		  || (echo "$$m does not build standalone (GOWORK=off) — a sync-promoted version is likely missing its go.sum /go.mod hash; run: cd $$m && GOWORK=off go mod download <module>" >&2 && exit 1); \
	done

check: work-sync
	@for s in $(MODULES); do \
		echo "==> $$s check"; \
		$(MAKE) -C $$s check || exit 1; \
	done

build:
	@for s in $(MODULES); do \
		echo "==> $$s build"; \
		$(MAKE) -C $$s build || exit 1; \
	done

test:
	@for s in $(MODULES); do \
		echo "==> $$s test"; \
		$(MAKE) -C $$s test || exit 1; \
	done

# Service-owned suites exercise each deployable component directly. Keep these
# exhaustive contracts beside the service that owns them.
service-e2e:
	$(MAKE) -C cli e2e
	$(MAKE) -C services/release-indexer e2e
	$(MAKE) -C services/release-indexer smoke

# Product journeys always cross the production gateway and run the complete
# stack, including PostgreSQL and the production n8n nodes image.
product-e2e:
	$(MAKE) -C e2e e2e

e2e: service-e2e product-e2e
