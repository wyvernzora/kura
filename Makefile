# Kura monorepo — root orchestration. Each service owns its Makefile;
# these targets fan out.

SERVICES := services/library services/releases services/webui

.PHONY: check build test

check:
	@for s in $(SERVICES); do \
		echo "==> $$s check"; \
		$(MAKE) -C $$s check || exit 1; \
	done

build:
	@for s in $(SERVICES); do \
		echo "==> $$s build"; \
		$(MAKE) -C $$s build || exit 1; \
	done

test:
	@for s in $(SERVICES); do \
		echo "==> $$s test"; \
		$(MAKE) -C $$s test || exit 1; \
	done
