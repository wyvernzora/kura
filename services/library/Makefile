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

.PHONY: build check fmt install lint test vet devserver-build devserver-run e2e

build:
	go build -o bin/kura ./cmd/kura

install: build
	go install ./cmd/kura

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
		$(DEVSERVER_IMAGE)
