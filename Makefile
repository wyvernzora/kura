# Kura monorepo — root orchestration. Each module owns its Makefile;
# these targets fan out.

MODULES := services/library-manager services/release-indexer services/webui cli

.PHONY: check build test

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
