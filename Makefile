GO ?= go
BIN := bin/aegis
WEB := web
WEBSTAMP := $(WEB)/node_modules/.installed

.PHONY: build test gen fmt vet lint tools crossbuild clean web dev

build: web
	$(GO) build -o $(BIN) ./cmd/aegis

web: $(WEBSTAMP)
	npm --prefix $(WEB) run build

$(WEBSTAMP): $(WEB)/package.json $(WEB)/package-lock.json
	npm --prefix $(WEB) ci
	@touch $(WEBSTAMP)

dev: $(WEBSTAMP)
	npm --prefix $(WEB) run dev

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
