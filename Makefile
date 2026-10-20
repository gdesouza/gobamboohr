# bamboohr — Go client and CLI for the BambooHR API.

BINARY      := bamboohr
PKG         := github.com/gdesouza/gobamboohr
CMD         := ./cmd/bamboohr
BIN_DIR     := bin
DIST_DIR    := dist
COVER_FILE  := coverage.out

# VERSION comes from the current git tag when there is one, otherwise from the
# short commit with a -dirty suffix for uncommitted work. Override it for a
# release build: make build VERSION=1.2.3
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

# Platforms built by `make release`.
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

GO       ?= go
GOFLAGS  ?=
TESTFLAGS ?= -race

.DEFAULT_GOAL := help

## help: list the available targets
.PHONY: help
help:
	@printf 'Usage: make <target> [VERSION=x.y.z]\n\nTargets:\n'
	@grep -E '^## [a-zA-Z_-]+:' $(MAKEFILE_LIST) \
		| sed -E 's/^## ([a-zA-Z_-]+): (.*)/\1|\2/' \
		| awk -F'|' '{printf "  %-14s %s\n", $$1, $$2}'
	@printf '\nVersion: %s\n' '$(VERSION)'

## build: compile the CLI into bin/
.PHONY: build
build:
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(CMD)
	@printf 'built %s/%s %s\n' '$(BIN_DIR)' '$(BINARY)' '$(VERSION)'

## install: build and install the CLI into GOBIN
.PHONY: install
install:
	$(GO) install $(GOFLAGS) -ldflags "$(LDFLAGS)" $(CMD)

## test: run the unit tests with the race detector
.PHONY: test
test:
	$(GO) test $(GOFLAGS) $(TESTFLAGS) ./...

## cover: run the tests and report coverage per package
.PHONY: cover
cover:
	$(GO) test $(GOFLAGS) -coverprofile=$(COVER_FILE) -covermode=atomic ./...
	@$(GO) tool cover -func=$(COVER_FILE) | tail -n 1
	@printf 'full report: go tool cover -html=%s\n' '$(COVER_FILE)'

## lint: check formatting, run go vet, and run golangci-lint when installed
.PHONY: lint
lint: fmt-check vet
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		printf 'golangci-lint not installed; ran gofmt and go vet only\n'; \
		printf 'install: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest\n'; \
	fi

## fmt: rewrite the source with gofmt
.PHONY: fmt
fmt:
	gofmt -s -w .

.PHONY: fmt-check
fmt-check:
	@unformatted=$$(gofmt -s -l .); \
	if [ -n "$$unformatted" ]; then \
		printf 'these files need gofmt -s -w:\n%s\n' "$$unformatted"; \
		exit 1; \
	fi

.PHONY: vet
vet:
	$(GO) vet $(GOFLAGS) ./...

## tidy: tidy go.mod and verify the dependencies
.PHONY: tidy
tidy:
	$(GO) mod tidy
	$(GO) mod verify

## release: cross-compile release archives into dist/
.PHONY: release
release: clean
	@mkdir -p $(DIST_DIR)
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		out=$(DIST_DIR)/$(BINARY)-$(VERSION)-$$os-$$arch; \
		printf 'building %s\n' "$$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			$(GO) build -trimpath -ldflags "$(LDFLAGS) -s -w" -o "$$out" $(CMD) || exit 1; \
		gzip -f "$$out"; \
	done
	@ls -1 $(DIST_DIR)

## clean: remove build output
.PHONY: clean
clean:
	rm -rf $(BIN_DIR) $(DIST_DIR) $(COVER_FILE)

## ci: everything a pipeline should run
.PHONY: ci
ci: lint test build
