GO_PACKAGES := ./...
GOFMT_DIRS := cmd internal

# VERSION stamps the kura CLI via -ldflags="-X main.Version=...".
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GO_LDFLAGS := -s -w -X main.Version=$(VERSION)

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

.PHONY: build check e2e fmt install lint test vet

build:
	go build -trimpath -ldflags='$(GO_LDFLAGS)' -o bin/kura ./cmd/kura

install: build
	go install -trimpath -ldflags='$(GO_LDFLAGS)' ./cmd/kura

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

e2e:
	go test -tags=e2e -v -race -count=1 -timeout=120s ./e2e/...

check: fmt vet lint test build
