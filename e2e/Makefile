.PHONY: check e2e fmt test vet

check: fmt vet test

fmt:
	gofmt -w .

vet:
	go vet ./...

test:
	go test ./...

e2e:
	mkdir -p ../artifacts/product-e2e
	go test -tags=e2e -v -count=1 -timeout=45m -artifacts \
		-outputdir=../artifacts/product-e2e ./...
