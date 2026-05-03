BINARY      := url-shortener
MAIN_PKG    := ./cmd/api
BIN_DIR     := bin
GOPATH_BIN  := $(shell go env GOPATH)/bin
SWAG        := $(GOPATH_BIN)/swag
MIGRATE     := $(GOPATH_BIN)/migrate
MIGRATIONS  := migrations

# Load DatabaseURL from .env if present so migrate targets can use it.
ifneq (,$(wildcard ./.env))
include .env
export
endif

GOLANGCI_LINT := $(GOPATH_BIN)/golangci-lint

.PHONY: help fmt vet lint docs run build clean tidy test test-unit test-integration test-bench test-fuzz \
        init ci \
        docker-build docker-run docker-shell compose-up compose-down compose-logs compose-up-otel \
        migrate-create migrate-up migrate-down migrate-version migrate-force

help:
	@echo "Targets:"
	@echo "  fmt              - format Go code (go fmt ./...)"
	@echo "  vet              - run go vet ./..."
	@echo "  lint             - run golangci-lint (installs if missing)"
	@echo "  docs             - regenerate Swagger docs into ./docs"
	@echo "  run              - run the API (go run $(MAIN_PKG))"
	@echo "  build            - build binary into $(BIN_DIR)/$(BINARY)"
	@echo "  tidy             - go mod tidy"
	@echo "  clean            - remove build artifacts"
	@echo "  test             - run all tests (unit + integration; integration skips without docker)"
	@echo "  test-unit        - run only unit tests (no docker, no DB)"
	@echo "  test-integration - run repository integration tests via testcontainers (requires docker)"
	@echo "  test-coverage    - run all tests with coverage; fails if total < $(COVERAGE_THRESHOLD)%"
	@echo "  test-bench       - run benchmarks (BENCH=<pattern>, default '.')"
	@echo "  test-fuzz        - run a fuzz target (FUZZ=<TestName>, FUZZTIME=<duration>)"
	@echo "  init             - set up local dev: install pre-commit hook (run once after clone)"
	@echo "  ci               - what CI runs locally: gofmt-check + vet + lint + test-coverage"
	@echo "  docker-build     - build the distroless image (tag: $(BINARY):dev)"
	@echo "  docker-run       - run the image standalone (expects Postgres+Redis already reachable)"
	@echo "  compose-up       - bring up the full stack via docker-compose"
	@echo "  compose-down     - tear it down (keeps the db-data volume)"
	@echo "  compose-logs     - tail logs for the api service"
	@echo "  compose-up-otel  - bring up the stack plus Jaeger + Prometheus override"
	@echo "  migrate-create   - create timestamped migration pair (name=<snake_case>)"
	@echo "  migrate-up       - apply all pending migrations"
	@echo "  migrate-down     - roll back the most recent migration"
	@echo "  migrate-version  - print current migration version"
	@echo "  migrate-force    - mark schema at version=<n> (recover from dirty state)"

fmt:
	go fmt ./...

vet:
	go vet ./...

$(GOLANGCI_LINT):
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run

# Mirror of CI's check stage; fails on the same things CI fails on. Useful
# before pushing if you want to know your branch will go green.
#
# Order matches GitHub Actions: format → vet → lint → build → test+coverage.
# Build comes before tests so a compile failure surfaces directly instead of
# as "test binary failed to build".
ci:
	@unfmt=$$(gofmt -l .); \
	  if [ -n "$$unfmt" ]; then echo "gofmt needed:"; echo "$$unfmt"; exit 1; fi
	go vet ./...
	$(MAKE) lint
	go build ./...
	$(MAKE) test-coverage

$(SWAG):
	go install github.com/swaggo/swag/cmd/swag@latest

docs: $(SWAG)
	$(SWAG) init -d ./cmd/api,./internal/api -g main.go -o docs

run:
	go run $(MAIN_PKG)

build:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY) $(MAIN_PKG)

tidy:
	go mod tidy

clean:
	rm -rf $(BIN_DIR)

test:
	go test -race ./...

# Unit tests only — service layer with fakes, pure-function tests, plus the
# platform/observability package (no external services needed). Skips the
# repository package, which needs Docker for testcontainers.
test-unit:
	go test -race -count=1 ./internal/service/... ./internal/api/... ./internal/platform/observability/...

# Spins up throwaway PostgreSQL containers via testcontainers and runs every
# package whose tests need a real DB:
#   - internal/repository: repo round-trips inside per-test transactions
#     (applies migrations on container boot)
#   - internal/platform/leader: advisory-lock contention tests (no migrations
#     needed — locks are application-table-free)
# Requires only Docker — no DATABASE_URL, no test DB to provision.
test-integration:
	go test -race -v -count=1 ./internal/repository/... ./internal/platform/leader/...

# Benchmarks. Override the pattern with BENCH=<regex>.
#   make test-bench                       # all benchmarks
#   make test-bench BENCH=ShortKey        # only matching ones
BENCH ?= .
test-bench:
	go test -run=^$$ -bench=$(BENCH) -benchmem ./...

# Fuzz a specific target. FUZZ is required; PKG defaults to the service
# package (where our fuzzers live). Go's fuzzer accepts only one target per
# invocation, so this can't span ./... — pass PKG=<dir> to switch packages.
#   make test-fuzz FUZZ=FuzzShortKeyFromURL
#   make test-fuzz FUZZ=FuzzShortKeyFromURL FUZZTIME=2m
#   make test-fuzz FUZZ=FuzzFoo PKG=./internal/api/...
FUZZTIME ?= 30s
PKG      ?= ./internal/service/...
test-fuzz:
	@if [ -z "$(FUZZ)" ]; then echo "FUZZ=<TestName> is required"; exit 1; fi
	go test -run=^$$ -fuzz=^$(FUZZ)$$ -fuzztime=$(FUZZTIME) $(PKG)

# Coverage gate. Runs the full suite (unit + integration) with -coverpkg
# pointing at every package in the module so cross-package coverage counts.
# Reads the total from `go tool cover -func` and exits non-zero when below
# COVERAGE_THRESHOLD. Override with COVERAGE_THRESHOLD=N make test-coverage.
COVERAGE_THRESHOLD ?= 60
COVERAGE_FILE      ?= coverage.out
test-coverage:
	@echo "→ running tests with coverage"
	go test -race -count=1 -coverprofile=$(COVERAGE_FILE) -coverpkg=./... ./...
	@total=$$(go tool cover -func=$(COVERAGE_FILE) | awk '/^total:/ { sub("%","",$$3); print $$3 }'); \
	  printf "\ntotal coverage: %s%%  (threshold: $(COVERAGE_THRESHOLD)%%)\n" "$$total"; \
	  awk -v t="$$total" -v th="$(COVERAGE_THRESHOLD)" 'BEGIN { exit !(t+0 >= th+0) }' \
	    || { echo "✗ coverage below threshold"; exit 1; }
	@echo "✓ coverage check passed"

# One-shot dev setup: run after a fresh clone. Currently just installs the
# pre-commit hook so it stays in sync with the version-controlled script.
# Add tool installs (swag, migrate, golangci-lint) here later if you want
# `make init` to fully bootstrap a workstation.
#
# Uses `git rev-parse --git-path hooks` rather than assuming .git/hooks so
# the target works in subdirectory checkouts, worktrees, and submodules.
init:
	@hooks_dir=$$(git rev-parse --git-path hooks 2>/dev/null) || { echo "not inside a git repo"; exit 1; }; \
	  abs_script=$$(cd scripts/git-hooks && pwd)/pre-commit; \
	  chmod +x "$$abs_script"; \
	  mkdir -p "$$hooks_dir"; \
	  ln -sf "$$abs_script" "$$hooks_dir/pre-commit"; \
	  echo "✓ pre-commit hook installed: $$hooks_dir/pre-commit -> $$abs_script"

# ----- Docker / compose -----

# Image tag pattern: $(BINARY):<tag>. VERSION/COMMIT get stamped into the
# binary via -ldflags so a built image carries identifying metadata.
DOCKER_TAG     ?= dev
DOCKER_VERSION ?= $(DOCKER_TAG)
DOCKER_COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
COMPOSE        ?= docker compose

docker-build:
	docker build \
		--build-arg VERSION=$(DOCKER_VERSION) \
		--build-arg COMMIT=$(DOCKER_COMMIT) \
		-t $(BINARY):$(DOCKER_TAG) \
		.

# Run the image standalone — the caller is responsible for reachable
# Postgres and Redis (set DatabaseURL / RedisURL via -e). For a turnkey
# local stack use `make compose-up` instead.
docker-run:
	docker run --rm -it \
		-p 8080:8080 -p 127.0.0.1:6060:6060 \
		--env-file .env \
		$(BINARY):$(DOCKER_TAG)

compose-up:
	$(COMPOSE) up --build -d
	@echo "API:        http://localhost:8080"
	@echo "Debug+/metrics (localhost only): http://localhost:6060"

compose-down:
	$(COMPOSE) down

compose-logs:
	$(COMPOSE) logs -f api

# Brings up the base stack plus the Jaeger + Prometheus override.
compose-up-otel:
	$(COMPOSE) -f docker-compose.yml -f docker-compose.observability.yml up --build -d
	@echo "API:        http://localhost:8080"
	@echo "Jaeger UI:  http://localhost:16686"
	@echo "Prometheus: http://localhost:9090"

$(MIGRATE):
	go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Usage: make migrate-create name=add_user_table
# Generates YYYYMMDDHHMMSS-prefixed up/down files (UTC).
migrate-create:
	@if [ -z "$(name)" ]; then echo "name=<snake_case> is required"; exit 1; fi
	@mkdir -p $(MIGRATIONS)
	@TS=$$(date -u +%Y%m%d%H%M%S); \
	UP=$(MIGRATIONS)/$${TS}_$(name).up.sql; \
	DOWN=$(MIGRATIONS)/$${TS}_$(name).down.sql; \
	: > $$UP; \
	: > $$DOWN; \
	echo "Created $$UP"; \
	echo "Created $$DOWN"

migrate-up: $(MIGRATE)
	@if [ -z "$(DatabaseURL)" ]; then echo "DatabaseURL is not set (check .env)"; exit 1; fi
	$(MIGRATE) -path $(MIGRATIONS) -database "$(DatabaseURL)" up

migrate-down: $(MIGRATE)
	@if [ -z "$(DatabaseURL)" ]; then echo "DatabaseURL is not set (check .env)"; exit 1; fi
	$(MIGRATE) -path $(MIGRATIONS) -database "$(DatabaseURL)" down 1

migrate-version: $(MIGRATE)
	@if [ -z "$(DatabaseURL)" ]; then echo "DatabaseURL is not set (check .env)"; exit 1; fi
	$(MIGRATE) -path $(MIGRATIONS) -database "$(DatabaseURL)" version

# Usage: make migrate-force version=20260503120001
migrate-force: $(MIGRATE)
	@if [ -z "$(DatabaseURL)" ]; then echo "DatabaseURL is not set (check .env)"; exit 1; fi
	@if [ -z "$(version)" ]; then echo "version=<n> is required"; exit 1; fi
	$(MIGRATE) -path $(MIGRATIONS) -database "$(DatabaseURL)" force $(version)
