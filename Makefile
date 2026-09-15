GO ?= go
BIN := bin/aegis

.PHONY: build test fmt vet lint tools crossbuild clean

build:
	$(GO) build -o $(BIN) ./cmd/aegis

test:
	$(GO) test -race ./...

crossbuild:
	./scripts/check-release-targets.sh

fmt:
	gofmt -l -w .

vet:
	$(GO) vet ./...

lint:
	golangci-lint run

tools:
	$(GO) install github.com/pressly/goose/v3/cmd/goose@latest
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	$(GO) install github.com/goreleaser/goreleaser/v2@latest

clean:
	rm -rf bin
