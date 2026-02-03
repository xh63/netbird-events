# Binary name
# Note: Binary is called 'eventsproc' (short CLI name)
# Repository is 'netbird-events' (descriptive GitHub name)
BIN_NAME=eventsproc

# Go binary
GO=go

# Version from version.txt (used by goreleaser)
MAIN_VERSION?=$$(cat version.txt)
ITERATION?=1

# Export for goreleaser
export MAIN_VERSION
export ITERATION

# Go files to format
GOFMT_FILES?=$$(find . -name '*.go' | grep -v vendor)

# Prettify gofmt errors
fmt:
	@echo "Running gofmt..."
	@gofmt -s -l -w $(GOFMT_FILES)

# Lint your code
# you need to have golang-ci-lint and golint installed in your PATH
# go get -u golang.org/x/lint/golint
# go get -u github.com/golangci/golangci-lint/cmd/golangci-lint or https://golangci-lint.run/welcome/install/#local-installation
lint:
	@echo "Running golangci-lint..."
	@golangci-lint run -v ./...

# Run tests and generate a coverage report
test:
	@echo "Running tests..."
	@$(GO) test -v -cover ./...

build:
	@echo "Building $(BIN_NAME) version $(MAIN_VERSION)..."
	MAIN_VERSION=$(MAIN_VERSION) ITERATION=$(ITERATION) goreleaser build --snapshot --single-target --clean --skip=validate

release:
	MAIN_VERSION=$(MAIN_VERSION) ITERATION=$(ITERATION) goreleaser release --clean --skip=validate

all: fmt lint test build

.PHONY: fmt lint test build release all
