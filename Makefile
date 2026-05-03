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

INSPECTOR_IMAGE ?= kura-inspector
LIBRARY_ROOT    ?= $(CURDIR)/testlib

.PHONY: build check fmt install lint test vet inspector-build inspector-run

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

inspector-build:
	docker build -f tools/inspector/Dockerfile -t $(INSPECTOR_IMAGE) .

inspector-run:
	@if [ ! -d "$(LIBRARY_ROOT)" ]; then \
		echo "LIBRARY_ROOT=$(LIBRARY_ROOT) does not exist; create it or pass LIBRARY_ROOT=/path/to/library"; \
		exit 1; \
	fi
	docker run --rm -it \
		-p 6274:6274 -p 6277:6277 \
		-v "$(LIBRARY_ROOT):/mnt/library" \
		$(if $(KURA_TVDB_KEY),-e KURA_TVDB_KEY="$(KURA_TVDB_KEY)") \
		$(INSPECTOR_IMAGE)
