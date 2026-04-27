# CodeGraph Go — Makefile

MODULE  := github.com/kristofer/codegraph
BINARY  := codegraph
BUILD_DIR := dist
CMD_DIR := ./cmd/codegraph

# Inject version from git tag (or "dev" when untagged).
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -trimpath -ldflags="-s -w -X main.Version=$(VERSION)"

# ─── Primary targets ─────────────────────────────────────────────────────────

.PHONY: all build test lint clean release

all: build

## build: Compile the binary for the host platform.
build:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) $(CMD_DIR)

## test: Run all tests with the race detector and coverage.
test:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	@go tool cover -func=coverage.out | tail -1

## lint: Run golangci-lint (requires golangci-lint to be installed).
lint:
	golangci-lint run ./...

## clean: Remove build artifacts.
clean:
	rm -rf $(BUILD_DIR) coverage.out

## tidy: Tidy and verify module dependencies.
tidy:
	go mod tidy
	go mod verify

# ─── Cross-compile release binaries ──────────────────────────────────────────

PLATFORMS := \
	linux/amd64 \
	linux/arm64 \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64 \
	windows/arm64

## release: Cross-compile binaries for all target platforms.
release:
	@mkdir -p $(BUILD_DIR)
	@for platform in $(PLATFORMS); do \
		GOOS=$$(echo $$platform | cut -d/ -f1); \
		GOARCH=$$(echo $$platform | cut -d/ -f2); \
		EXT=""; \
		if [ "$$GOOS" = "windows" ]; then EXT=".exe"; fi; \
		OUT=$(BUILD_DIR)/$(BINARY)-$$GOOS-$$GOARCH$$EXT; \
		echo "Building $$OUT …"; \
		GOOS=$$GOOS GOARCH=$$GOARCH go build $(LDFLAGS) -o $$OUT $(CMD_DIR) || exit 1; \
	done
	@echo "Release binaries written to $(BUILD_DIR)/"

# ─── Help ─────────────────────────────────────────────────────────────────────

## help: Display this help message.
help:
	@grep -E '^## ' Makefile | sed 's/^## //'
