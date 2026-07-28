# Kura monorepo — root orchestration. Each module owns its Makefile;
# these targets fan out.

MODULES := services/library-manager services/release-indexer services/gateway services/gateway/web cli

.PHONY: check build test service-e2e product-e2e e2e

check:
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
# non-n8n stack, including PostgreSQL.
product-e2e:
	$(MAKE) -C e2e e2e

e2e: service-e2e product-e2e
