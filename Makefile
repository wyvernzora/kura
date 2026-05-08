GO_PACKAGES := ./...
GOFMT_DIRS := cmd internal

# golangci-lint replaces the prior gopls-check workflow. Resolution
# order: PATH first, then $GOBIN, then $GOPATH/bin so a `go install`
# without exporting GOBIN still finds the binary.
GOLANGCI_LINT ?= $(shell if command -v golangci-lint >/dev/null 2>&1; then \
		command -v golangci-lint; \
	else \
		GOBIN=$$(go env GOBIN); GOPATH=$$(go env GOPATH); \
		if [ -n "$$GOBIN" ] && [ -x "$$GOBIN/golangci-lint" ]; then \
			printf "%s/golangci-lint" "$$GOBIN"; \
		elif [ -x "$$GOPATH/bin/golangci-lint" ]; then \
			printf "%s/bin/golangci-lint" "$$GOPATH"; \
		fi; \
	fi)

DEVSERVER_IMAGE      ?= kura-devserver
REST_DEV_PORT        ?= 8080
MCP_HTTP_PORT        ?= 8081
INSPECTOR_PORT       ?= 6274
INSPECTOR_PROXY_PORT ?= 6277
VITE_PORT            ?= 5173
STORYBOOK_PORT       ?= 6006

WEB_DIR          := web
WEB_DIST         := $(WEB_DIR)/dist
WEBUI_EMBED_DIST := internal/server/webui/dist

.PHONY: build check fmt install lint test vet devserver-build devserver-run e2e \
	web-install web-install-if-missing web-build web-dev web-test web-typecheck web-clean \
	storybook-dev storybook-build release

build:
	go build -o bin/kura ./cmd/kura

# `install` always rebuilds the embedded web bundle so the resulting
# binary actually serves the UI (otherwise `bin/kura` falls back to
# the tracked placeholder index.html and surfaces a "Web UI bundle
# not built" message). web-install-if-missing skips the ~2 s pnpm
# install when node_modules is already populated, so repeat installs
# only pay for `pnpm build` (~1 s).
install: web-install-if-missing web-build build
	go install ./cmd/kura

# Web UI build flow:
#
#   web-install       — pnpm install with frozen lockfile.
#   web-build         — vite build + rsync dist/ into the embed package.
#                       Note: this overwrites the tracked placeholder
#                       internal/server/webui/dist/index.html with the
#                       real bundle. Run `make web-clean` to restore.
#   web-dev           — Vite dev server on :$(VITE_PORT) with /api proxy
#                       to :$(REST_DEV_PORT).
#   web-typecheck     — `tsc -b` (no emit).
#   web-test          — Vitest run.
#   web-clean         — wipe build outputs and restore the tracked
#                       placeholder so go build still succeeds.
#   storybook-dev     — Storybook on :$(STORYBOOK_PORT).
#   storybook-build   — Static Storybook export to web/storybook-static.
#   release           — web-build → embed → go build (single binary
#                       carrying the freshly bundled UI).
web-install:
	cd $(WEB_DIR) && pnpm install --frozen-lockfile

# Idempotent guard for `make install`: if pnpm has never populated
# node_modules in this checkout, run `web-install`; otherwise skip
# (the ~2 s install dominates a no-op build when chained on every
# call). Lockfile drift is the user's job to resolve via `web-install`.
web-install-if-missing:
	@if [ ! -d $(WEB_DIR)/node_modules/.pnpm ]; then \
		echo "==> web/node_modules missing — running web-install"; \
		$(MAKE) web-install; \
	fi

web-build:
	cd $(WEB_DIR) && pnpm build
	rsync -a --delete $(WEB_DIST)/ $(WEBUI_EMBED_DIST)/

web-dev:
	cd $(WEB_DIR) && pnpm dev

web-typecheck:
	cd $(WEB_DIR) && pnpm typecheck

web-test:
	cd $(WEB_DIR) && pnpm test

web-clean:
	rm -rf $(WEB_DIST) $(WEB_DIR)/storybook-static
	git checkout -- $(WEBUI_EMBED_DIST)

storybook-dev:
	cd $(WEB_DIR) && pnpm storybook

storybook-build:
	cd $(WEB_DIR) && pnpm build-storybook

release: web-build build

fmt:
	gofmt -w $(GOFMT_DIRS)

vet:
	go vet $(GO_PACKAGES)

lint:
	@if [ -z "$(GOLANGCI_LINT)" ]; then \
		echo "golangci-lint not found. Install it with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 127; \
	fi
	$(GOLANGCI_LINT) run $(GO_PACKAGES)

test:
	go test $(GO_PACKAGES)

check: fmt vet lint test build

e2e:
	go test -tags=e2e -v -race -count=1 -timeout=120s ./e2e/...

# Combined hot-reload dev container: REST + MCP HTTP + Inspector UI.
# See tools/devserver/README.md for usage.
devserver-build:
	docker build -f tools/devserver/Dockerfile -t $(DEVSERVER_IMAGE) .

devserver-run:
	@if [ -n "$(KURA_LIBRARY_ROOT)" ] && [ ! -d "$(KURA_LIBRARY_ROOT)" ]; then \
		echo "KURA_LIBRARY_ROOT=$(KURA_LIBRARY_ROOT) does not exist; create it or unset to use the ephemeral container library"; \
		exit 1; \
	fi
	@if [ -n "$(KURA_INBOX_ROOT)" ] && [ ! -d "$(KURA_INBOX_ROOT)" ]; then \
		echo "KURA_INBOX_ROOT=$(KURA_INBOX_ROOT) does not exist; create it or unset to skip the inbox mount"; \
		exit 1; \
	fi
	docker run --rm -it \
		-p 127.0.0.1:$(REST_DEV_PORT):8080 \
		-p 127.0.0.1:$(MCP_HTTP_PORT):8081 \
		-p 127.0.0.1:$(INSPECTOR_PORT):6274 \
		-p 127.0.0.1:$(INSPECTOR_PROXY_PORT):6277 \
		-p 127.0.0.1:$(VITE_PORT):5173 \
		-p 127.0.0.1:$(STORYBOOK_PORT):6006 \
		-v "$(CURDIR):/src" \
		-v /src/web/node_modules \
		$(if $(KURA_LIBRARY_ROOT),-v "$(KURA_LIBRARY_ROOT):/mnt/library") \
		$(if $(KURA_INBOX_ROOT),-v "$(KURA_INBOX_ROOT):/mnt/inbox") \
		$(if $(KURA_TVDB_KEY),-e KURA_TVDB_KEY="$(KURA_TVDB_KEY)") \
		$(if $(KURA_DEV_STUBS),-e KURA_DEV_STUBS="$(KURA_DEV_STUBS)") \
		$(if $(KURA_PREFERRED_LANGUAGES),-e KURA_PREFERRED_LANGUAGES="$(KURA_PREFERRED_LANGUAGES)") \
		$(if $(KURA_LOG_LEVEL),-e KURA_LOG_LEVEL="$(KURA_LOG_LEVEL)") \
		$(if $(KURA_REST_CORS_ORIGINS),-e KURA_REST_CORS_ORIGINS="$(KURA_REST_CORS_ORIGINS)") \
		$(if $(KURA_TOKEN),-e KURA_TOKEN="$(KURA_TOKEN)") \
		$(if $(KURA_DISABLE_TOKEN),-e KURA_DISABLE_TOKEN="$(KURA_DISABLE_TOKEN)") \
		$(if $(KURA_WEB_DISABLED),-e KURA_WEB_DISABLED="$(KURA_WEB_DISABLED)") \
		-e CHOKIDAR_USEPOLLING=1 \
		-e CHOKIDAR_INTERVAL=300 \
		-e WATCHPACK_POLLING=true \
		$(DEVSERVER_IMAGE)
