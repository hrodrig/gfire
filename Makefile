APP_NAME := gfire
BIN_DIR := bin
IMAGE ?= $(APP_NAME):local
GOPATH_BIN := $(shell go env GOPATH)/bin
PREFIX ?= $(GOPATH_BIN)
BINDIR ?= $(PREFIX)

VERSION := $(shell cat VERSION 2>/dev/null | tr -d ' \n\r' || echo dev)
GIT_COMMIT := $(firstword $(shell git rev-parse --short HEAD 2>/dev/null | head -1))
ifeq ($(strip $(GIT_COMMIT)),)
  GIT_COMMIT := unknown
endif
GIT_BRANCH := $(firstword $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null | head -1 | tr -cs 'A-Za-z0-9._-' '_'))
ifeq ($(strip $(GIT_BRANCH)),)
  GIT_BRANCH := unknown
endif
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -s -w -X 'github.com/hrodrig/gfire/internal/version.Version=$(VERSION)' -X 'github.com/hrodrig/gfire/internal/version.Commit=$(GIT_COMMIT)' -X 'github.com/hrodrig/gfire/internal/version.Branch=$(GIT_BRANCH)' -X 'github.com/hrodrig/gfire/internal/version.BuildDate=$(BUILD_DATE)'

TAG := v$(VERSION)
COVER_MIN ?= 80
GRYPE_FAIL_ON ?= high
GRYPE_DIR_EXCLUDES := --exclude './bin/**' --exclude './dist/**' --exclude './docs/**'
STRICT_RELEASE ?= 0

GFIRE_CONFIG ?= gfire.yaml

PG_DSN ?= postgres://gfire:gfire@localhost:5432/gfire?sslmode=disable
MIGRATE_PATH := internal/storage/postgres/migrations

DOCKER_BUILD_ARGS := \
	--build-arg APP_VERSION=$(VERSION) \
	--build-arg GIT_COMMIT=$(GIT_COMMIT) \
	--build-arg GIT_BRANCH=$(GIT_BRANCH) \
	--build-arg BUILD_DATE=$(BUILD_DATE)

.DEFAULT_GOAL := help

.PHONY: help all build version test cover fmt fmt-check lint-fix lint vet clean install server \\
	docker-build docker-scan govulncheck vulncheck ci gocyclo grype security release-check snapshot \\
	db-up db-down db-psql migrate-up migrate-down migrate-create e2e

help:
	@echo "Available targets:"
	@echo "  make all            fmt, vet, test, gocyclo, cover, build"
	@echo "  make build          Build local binary to bin/$(APP_NAME)"
	@echo "  make version        Build and print version metadata"
	@echo "  make test           go test ./... -count=1"
	@echo "  make cover          Coverage report; gate with COVER_MIN (default $(COVER_MIN))"
	@echo "  make fmt            gofmt -w ."
	@echo "  make lint-fix       gofmt -s -w ."
	@echo "  make fmt-check      Fail if gofmt -s would change any file"
	@echo "  make lint           go vet ./... (alias of vet)"
	@echo "  make vet            go vet ./..."
	@echo "  make gocyclo        Fail if cyclomatic complexity > 15"
	@echo "  make govulncheck    Vulnerability scan via go run"
	@echo "  make grype          Grype directory scan (Docker fallback if missing)"
	@echo "  make security       govulncheck + gocyclo + grype"
	@echo "  make ci             fmt-check + lint + gocyclo + test"
	@echo "  make release-check  semver + goreleaser check + fmt + lint + test + cover + security"
	@echo "  make snapshot       Goreleaser snapshot to dist/ (no tag)"
	@echo "  make docker-build   Build container image"
	@echo "  make docker-scan    docker-build + Grype image scan"
	@echo "  make install        Install binary to $(BINDIR)"
	@echo "  make server         Build and run gfire server (GFIRE_CONFIG=$(GFIRE_CONFIG))"
	@echo "  make clean          Remove bin/ artifacts and coverage.out"
	@echo "  make db-up          Start postgres/redis/valkey via docker compose"
	@echo "  make db-down        Stop compose stack"
	@echo "  make migrate-up     Apply PostgreSQL migrations"
	@echo "  make migrate-down   Roll back one migration"
	@echo "  make e2e            Run end-to-end tests (postgres + curl + CLI)"
	@echo "  Redis tests: make db-up && go test ./internal/storage/redis/ -count=1"
	@echo ""
	@echo "Variables:"
	@echo "  COVER_MIN=<n>       Minimum coverage %% (default: $(COVER_MIN))"
	@echo "  GRYPE_FAIL_ON=      Grype severity gate (default: $(GRYPE_FAIL_ON))"
	@echo "  IMAGE=<name:tag>    Docker image tag (default: $(IMAGE))"
	@echo "  PG_DSN=<dsn>        Postgres DSN for migrate (default: local gfire)"
	@echo "  GFIRE_CONFIG=<path> Config for make server (default: gfire.yaml)"
	@echo "  STRICT_RELEASE=1    release-check also runs docker-scan"

all: fmt vet test gocyclo cover build

build:
	mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME) ./cmd/$(APP_NAME)

version: build
	./$(BIN_DIR)/$(APP_NAME) version

test:
	go test ./... -count=1

# Memory backend is the unit-test gate today; postgres needs a live DB (make db-up).
cover:
	go test ./internal/storage/memory/ -count=1 -coverprofile=coverage.out
	@P=$$(go tool cover -func=coverage.out | tail -1 | sed 's/^.*[[:space:]]\([0-9.]*\)%.*/\1/'); \
		echo "memory backend statement coverage: $$P% (minimum $(COVER_MIN)%)"; \
		if [ "$(COVER_MIN)" -gt 0 ]; then \
			command -v bc >/dev/null 2>&1 || { echo "COVER_MIN>0 requires bc"; exit 1; }; \
			if [ "$$(echo "$$P < $(COVER_MIN)" | bc)" -eq 1 ]; then \
				echo "coverage below $(COVER_MIN)%"; exit 1; \
			fi; \
		fi

fmt:
	gofmt -w .

lint-fix:
	gofmt -s -w .

fmt-check:
	@out=$$(gofmt -s -l .); \
	if [ -n "$$out" ]; then \
		echo "Run: make lint-fix"; \
		echo "$$out"; \
		exit 1; \
	fi

lint: vet

vet:
	go vet ./...

gocyclo:
	@files=$$(find . -name '*.go' -not -name '*_test.go' -not -path './.git/*' -not -path './bin/*'); \
	go run github.com/fzipp/gocyclo/cmd/gocyclo@latest -over 15 $$files

govulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

vulncheck: govulncheck

grype:
	@if command -v grype >/dev/null 2>&1; then \
		grype dir:. $(GRYPE_DIR_EXCLUDES) --fail-on $(GRYPE_FAIL_ON); \
	else \
		echo "grype not found locally, using container image..."; \
		docker run --rm --pull=always -v "$(PWD):/workspace" anchore/grype:latest \
			dir:/workspace $(GRYPE_DIR_EXCLUDES) --fail-on $(GRYPE_FAIL_ON); \
	fi

security: govulncheck gocyclo grype
	@echo "OK: security (govulncheck, gocyclo, grype)"

ci: fmt-check lint gocyclo test
	@echo "OK: ci (fmt-check, vet, gocyclo, test)"

# Semver + goreleaser + fmt-check + vet + test + cover + security (+ docker-scan when STRICT_RELEASE=1).
# Fail-closed: red gates must abort before any tag publish (see .github/workflows/release.yml).
release-check:
	@test -f VERSION || { echo "VERSION file is required"; exit 1; }
	@echo "Release version: $(VERSION) (tag: $(TAG))"
	@echo "$(VERSION)" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$$' || { echo "VERSION must be semver (e.g. 0.2.0)"; exit 1; }
	@git rev-parse --git-dir >/dev/null 2>&1 || { echo "release-check requires a git repository (clone or: git init && git remote add origin <url>)"; exit 1; }
	@git remote get-url origin >/dev/null 2>&1 || { echo "release-check requires git remote origin (GoReleaser scm validation)"; exit 1; }
	@command -v goreleaser >/dev/null 2>&1 || { echo "goreleaser is required. Install from https://goreleaser.com/install/"; exit 1; }
	@test -f .goreleaser.yaml || { echo ".goreleaser.yaml is required"; exit 1; }
	goreleaser check
	@$(MAKE) fmt-check
	@$(MAKE) lint
	@$(MAKE) test
	@$(MAKE) cover
	@$(MAKE) security
	@if [ "$(STRICT_RELEASE)" = "1" ]; then \
		echo "STRICT_RELEASE=1 -> running docker-scan"; \
		$(MAKE) docker-scan; \
	else \
		echo "STRICT_RELEASE=0 -> skipping docker-scan"; \
	fi
	@echo "All release checks passed."

snapshot:
	@ver_raw=$$(cat VERSION 2>/dev/null | tr -d '\n\r'); \
	[ -n "$$ver_raw" ] || { echo "Error: VERSION file is required for snapshot"; exit 1; }; \
	ver=$${ver_raw#v}; \
	echo "$$ver" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$$' || { echo "Error: VERSION must be semantic MAJOR.MINOR.PATCH (got: $$ver_raw)"; exit 1; }; \
	goreleaser release --snapshot --clean

clean:
	rm -f coverage.out
	@mkdir -p "$(BIN_DIR)"
	@for f in $$(find "$(BIN_DIR)" -mindepth 1 -maxdepth 1 ! -name '.keep' 2>/dev/null); do rm -rf "$$f"; done

install: build
	install -d "$(BINDIR)"
	install -m 755 $(BIN_DIR)/$(APP_NAME) "$(BINDIR)/$(APP_NAME)"
	@echo "Installed $(APP_NAME) to $(BINDIR)."

server: build
	@if [ -n "$(GFIRE_CONFIG)" ] && [ ! -f "$(GFIRE_CONFIG)" ] && [ -f gfire.example.yaml ]; then \
		cp gfire.example.yaml "$(GFIRE_CONFIG)"; \
		echo "Created $(GFIRE_CONFIG) from gfire.example.yaml"; \
	fi
	@if [ -n "$(GFIRE_CONFIG)" ]; then \
		./$(BIN_DIR)/$(APP_NAME) server --config "$(GFIRE_CONFIG)"; \
	else \
		./$(BIN_DIR)/$(APP_NAME) server; \
	fi

# ──────────────────────────────────────────────
# Docker
# ──────────────────────────────────────────────

docker-build:
	docker build $(DOCKER_BUILD_ARGS) -t $(IMAGE) -t $(APP_NAME):$(VERSION) -f Dockerfile .

docker-scan: docker-build
	@if command -v grype >/dev/null 2>&1; then \
		grype $(IMAGE) --fail-on $(GRYPE_FAIL_ON); \
	else \
		echo "grype not found locally, using container image..."; \
		docker run --rm --pull=always -v /var/run/docker.sock:/var/run/docker.sock anchore/grype:latest \
			$(IMAGE) --fail-on $(GRYPE_FAIL_ON); \
	fi

# ──────────────────────────────────────────────
# Development helpers (storage)
# ──────────────────────────────────────────────

db-up:
	docker compose up -d postgres redis valkey

db-down:
	docker compose down

db-psql:
	psql "$(PG_DSN)"

migrate-create:
	@read -p "Migration name: " name; \
	migrate create -ext sql -dir $(MIGRATE_PATH) -seq $$name

migrate-up:
	migrate -path $(MIGRATE_PATH) -database "$(PG_DSN)" up

migrate-down:
	migrate -path $(MIGRATE_PATH) -database "$(PG_DSN)" down 1

e2e:
	@echo "Running E2E tests (requires docker + migrate CLI)..."
	bash test/e2e/run.sh
