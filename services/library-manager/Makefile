GO_PACKAGES := ./...
GOFMT_DIRS := cmd internal

# VERSION stamps the kura binary via -ldflags="-X main.Version=...".
# Defaults to `git describe` (tag-based, falls back to short SHA on an
# untagged checkout) so dev builds carry a meaningful identifier without
# needing a manual override. Tagged releases get the literal tag, e.g.
# "v0.5.1"; mid-development checkouts get something like
# "v0.5.1-3-g35093c0" or "v0.5.1-3-g35093c0-dirty". CI/release builds
# can pass `VERSION=v0.5.1` explicitly.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GO_LDFLAGS := -s -w -X main.Version=$(VERSION)

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

.PHONY: build check check-gen fmt gen-ts install lint test vet devserver-build devserver-run e2e

build:
	go build -trimpath -ldflags='$(GO_LDFLAGS)' -o bin/kura-library-manager ./cmd/kura-library-manager

# gen-ts regenerates ../webui/src/api/types.gen.ts from internal/response.
# The wrapper at tools/tygo runs tygo and post-processes the closed
# string-enum aliases (Status, ListStatus, ScanStatus) into TS literal
# unions — tygo emits them as `= string` otherwise.
gen-ts:
	cd tools/tygo && go run .
	cd ../webui && pnpm exec biome check --write src/api/types.gen.ts >/dev/null

# check-gen regenerates and fails if the committed file drifted. Wired
# into `check` so PRs that change response types without running gen-ts
# fail in CI. Assumes ../webui/src/api/types.gen.ts is tracked — the first
# commit that lands tygo must include it.
check-gen: gen-ts
	@git diff --exit-code -- ../webui/src/api/types.gen.ts \
	  || (echo "types.gen.ts is stale; run 'make gen-ts' and commit the result" && exit 1)

install: build
	go install -trimpath -ldflags='$(GO_LDFLAGS)' ./cmd/kura-library-manager

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

check: fmt vet lint test build check-gen

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
		-v "$(CURDIR):/src" \
		$(if $(KURA_LIBRARY_ROOT),-v "$(KURA_LIBRARY_ROOT):/mnt/library") \
		$(if $(KURA_INBOX_ROOT),-v "$(KURA_INBOX_ROOT):/mnt/inbox") \
		$(if $(KURA_TVDB_KEY),-e KURA_TVDB_KEY="$(KURA_TVDB_KEY)") \
		$(if $(KURA_DEV_STUBS),-e KURA_DEV_STUBS="$(KURA_DEV_STUBS)") \
		$(if $(KURA_PREFERRED_LANGUAGES),-e KURA_PREFERRED_LANGUAGES="$(KURA_PREFERRED_LANGUAGES)") \
		$(if $(KURA_LOG_LEVEL),-e KURA_LOG_LEVEL="$(KURA_LOG_LEVEL)") \
		$(if $(KURA_REST_CORS_ORIGINS),-e KURA_REST_CORS_ORIGINS="$(KURA_REST_CORS_ORIGINS)") \
		$(if $(KURA_TOKEN),-e KURA_TOKEN="$(KURA_TOKEN)") \
		$(if $(KURA_DISABLE_TOKEN),-e KURA_DISABLE_TOKEN="$(KURA_DISABLE_TOKEN)") \
		-e CHOKIDAR_USEPOLLING=1 \
		-e CHOKIDAR_INTERVAL=300 \
		-e WATCHPACK_POLLING=true \
		$(DEVSERVER_IMAGE)
