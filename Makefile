GO ?= go
BIN := bin/aegis
WEB := web
WEBSTAMP := $(WEB)/node_modules/.installed

# Local dev stack. Override any of these: make dev DNS_ADDRESS=127.0.0.1:15399
API_ADDRESS ?= 127.0.0.1:8080
DNS_ADDRESS ?= 127.0.0.1:15353
DEV_DB ?= bin/dev.db

.PHONY: build server test check gen fmt vet lint mutants tools crossbuild clean web dev run

build: web
	$(GO) build -o $(BIN) ./cmd/aegis

server:
	$(GO) build -o $(BIN) ./cmd/aegis

web: $(WEBSTAMP)
	npm --prefix $(WEB) run build

$(WEBSTAMP): $(WEB)/package.json $(WEB)/package-lock.json
	npm --prefix $(WEB) ci
	@touch $(WEBSTAMP)

# dev runs the whole stack: the aegis server (API + DNS) behind the vite proxy,
# then vite in the foreground. Ctrl-C stops both. The dev database persists in
# bin/ between runs; make clean wipes it.
dev: server $(WEBSTAMP)
	@mkdir -p $(dir $(DEV_DB))
	@$(BIN) serve \
		--api-address $(API_ADDRESS) \
		--dns-address $(DNS_ADDRESS) \
		--db $(DEV_DB) & \
	srv_pid=$$!; \
	trap 'kill $$srv_pid 2>/dev/null' EXIT INT TERM HUP; \
	ready=0; \
	for i in $$(seq 1 50); do \
		curl -sf http://$(API_ADDRESS)/api/v1/status > /dev/null && { ready=1; break; }; \
		sleep 0.1; \
	done; \
	[ $$ready -eq 1 ] || { echo "aegis did not come up on $(API_ADDRESS)"; exit 1; }; \
	echo "aegis   API $(API_ADDRESS)   DNS $(DNS_ADDRESS)   db $(DEV_DB)"; \
	npm --prefix $(WEB) run dev

# run starts only the server, foreground, with the dev database. Dig it:
#   dig -p 15353 @127.0.0.1 example.com
run: server
	@mkdir -p $(dir $(DEV_DB))
	$(BIN) serve \
		--api-address $(API_ADDRESS) \
		--dns-address $(DNS_ADDRESS) \
		--db $(DEV_DB)

# check runs every local gate: Go tests with the race detector, vet, lint,
# the web unit tests, and the web typecheck.
check: test vet lint
	npm --prefix $(WEB) run test
	npm --prefix $(WEB) run typecheck

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

mutants:
	@for p in ./internal/filter ./internal/blocklist ./internal/services ./internal/metrics ./internal/ratelimit ./internal/config; do \
		gremlins unleash $$p || exit 1; \
	done

tools:
	$(GO) install github.com/pressly/goose/v3/cmd/goose@latest
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	$(GO) install github.com/goreleaser/goreleaser/v2@v2.17.0
	$(GO) install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	$(GO) install github.com/go-gremlins/gremlins/cmd/gremlins@latest

clean:
	rm -rf bin
