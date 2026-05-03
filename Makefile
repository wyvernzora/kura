GO_PACKAGES := ./...
GOFMT_DIRS := cmd internal
GO_FILES := $(shell find $(GOFMT_DIRS) -name '*.go' -type f | sort)

GOPLS ?= $(shell if command -v gopls >/dev/null 2>&1; then \
		command -v gopls; \
	else \
		GOBIN=$$(go env GOBIN); GOPATH=$$(go env GOPATH); \
		if [ -n "$$GOBIN" ] && [ -x "$$GOBIN/gopls" ]; then \
			printf "%s/gopls" "$$GOBIN"; \
		elif [ -x "$$GOPATH/bin/gopls" ]; then \
			printf "%s/bin/gopls" "$$GOPATH"; \
		fi; \
	fi)

.PHONY: build check fmt install lint test vet

build:
	go build -o bin/kura ./cmd/kura

install: build
	go install ./cmd/kura

fmt:
	gofmt -w $(GOFMT_DIRS)

vet:
	go vet $(GO_PACKAGES)

lint:
	@if [ -z "$(GOPLS)" ]; then \
		echo "gopls not found. Install it with: go install golang.org/x/tools/gopls@latest"; \
		exit 127; \
	fi
	@output="$$( $(GOPLS) check -severity=hint $(GO_FILES) 2>&1 )"; status=$$?; \
	if [ $$status -ne 0 ]; then \
		printf "%s\n" "$$output"; \
		exit $$status; \
	fi; \
	if [ -n "$$output" ]; then \
		printf "%s\n" "$$output"; \
		exit 1; \
	fi

test:
	go test $(GO_PACKAGES)

check: fmt vet lint test build
