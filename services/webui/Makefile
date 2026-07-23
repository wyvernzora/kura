.PHONY: install install-if-missing build check lint typecheck test dev

install:
	pnpm install --frozen-lockfile

install-if-missing:
	@if [ ! -d node_modules/.pnpm ]; then $(MAKE) install; fi

build:
	pnpm build

check: install-if-missing build lint typecheck test

lint:
	pnpm lint

typecheck:
	pnpm typecheck

test:
	pnpm test

dev:
	pnpm dev
