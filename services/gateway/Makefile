.PHONY: check fmt vet lint test build web-check

GO_LDFLAGS := -s -w -X main.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

check: fmt vet lint test build

fmt:
	gofmt -w cmd internal

vet:
	go vet ./...

lint:
	golangci-lint run ./...

test:
	go test ./...

build:
	go build -trimpath -ldflags='$(GO_LDFLAGS)' -o bin/kura-mcp-bridge ./cmd/kura-mcp-bridge

# The SPA is its own toolchain; the root Makefile drives it separately.
web-check:
	$(MAKE) -C web check
