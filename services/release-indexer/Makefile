GO_PACKAGES := ./...
GOFMT_DIRS  := cmd internal pkg sources

# VERSION and COMMIT stamp the binary via -ldflags.
# Defaults to `git describe` so dev builds carry a meaningful identifier
# without a manual override. CI/release builds pass VERSION=vX.Y.Z explicitly.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
GO_LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

# golangci-lint resolution order: PATH → $GOBIN → $GOPATH/bin.
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

.PHONY: build check devserver e2e fmt lint test vet smoke hooks

build:
	go build -trimpath -ldflags='$(GO_LDFLAGS)' -o bin/kura-release-indexer ./cmd/kura-release-indexer

fmt:
	gofmt -w $(GOFMT_DIRS)

vet:
	go vet $(GO_PACKAGES)

lint:
	@if [ -z "$(GOLANGCI_LINT)" ]; then \
		echo "golangci-lint not found. Install it with: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"; \
		exit 127; \
	fi
	$(GOLANGCI_LINT) run $(GO_PACKAGES)

test:
	go test $(GO_PACKAGES)
	$(MAKE) e2e

e2e:
	go test -tags=e2e -run TestEndToEndWorkflow -count=1 ./e2e

check: fmt vet lint test build

devserver:
	docker compose -f tools/devserver/compose.yaml up --build

# Real-binary smoke (cmd/kura-release-indexer/smoke_test.go, //go:build smoke): builds + runs the
# actual binary against a testcontainers Postgres and asserts the wired deploy shape
# (migrations -> /healthz -> /ingest -> /queue -> /mcp -> fail-fast bind -> bounded
# drain). Needs Docker; kept OUT of `check` so the default loop stays container-free.
# CI runs this same command in the dedicated `smoke` job.
smoke:
	go test -tags=smoke -run TestSmoke -count=1 ./cmd/kura-release-indexer

# Install lefthook-managed commit-msg and pre-commit hooks.
hooks:
	./scripts/install-githooks.sh
