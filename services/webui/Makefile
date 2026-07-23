.PHONY: install build check lint typecheck test dev

install:
	pnpm install --frozen-lockfile

build:
	pnpm build

check: build lint typecheck test

lint:
	pnpm lint

typecheck:
	pnpm typecheck

test:
	pnpm test

dev:
	pnpm dev
