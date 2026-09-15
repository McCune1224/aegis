GO ?= go
BIN := bin/aegis

.PHONY: build test gen fmt vet lint tools crossbuild clean

build:
	$(GO) build -o $(BIN) ./cmd/aegis

test:
	$(GO) test -race ./...

gen:
	sqlc generate

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
	$(GO) install github.com/sqlc-dev/sqlc/cmd/sqlc@latest

clean:
	rm -rf bin
