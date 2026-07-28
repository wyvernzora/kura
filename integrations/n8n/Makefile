PNPM := corepack pnpm

.PHONY: install install-if-missing check build typecheck test

install:
	$(PNPM) install --frozen-lockfile

install-if-missing:
	@if [ ! -d node_modules/.pnpm ]; then $(MAKE) install; fi

check: install-if-missing typecheck build test

build: install-if-missing
	$(PNPM) build

typecheck: install-if-missing
	$(PNPM) typecheck

test: install-if-missing build
	$(PNPM) test
