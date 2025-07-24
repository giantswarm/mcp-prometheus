# DO NOT EDIT. Generated with:
#
# devctl
#
# https://github.com/giantswarm/devctl/blob/eea19f200d7cfd27ded22474b787563bbfdb8ec4/pkg/gen/input/makefile/internal/file/Makefile.gen.go.mk.template
#

APPLICATION    := mcp-prometheus
BUILDTIMESTAMP := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
GITSHA1        := $(shell git rev-parse --short HEAD)
MODULE         := $(shell go list -m)
OS             := $(shell go env GOOS)
SOURCES        := $(shell find . -name '*.go')
VERSION        := $(shell architect project version)
ifeq ($(VERSION),)
VERSION := $(shell cat VERSION 2>/dev/null || echo dev)
endif
# Add the git hash to the version string if we're using the dev build
ifeq ($(VERSION), dev)
VERSION := $(shell echo "$(VERSION)+$(GITSHA1)")
endif

# go build flags
LDFLAGS := -w -linkmode 'auto' -extldflags '-static' \
  -X '$(shell go list -m)/internal/project.buildTime=$(BUILDTIMESTAMP)' \
  -X '$(shell go list -m)/internal/project.gitSHA=$(GITSHA1)' \
  -X '$(shell go list -m)/internal/project.version=$(VERSION)'

.DEFAULT_GOAL := build

##@ Go

all: build
.PHONY: all

build: $(APPLICATION) ## Builds a local binary.
.PHONY: build

$(APPLICATION): $(SOURCES) go.mod
	CGO_ENABLED=0 GOOS=$(OS) go build -ldflags "$(LDFLAGS)" -o $(APPLICATION) .

build-darwin: $(SOURCES) go.mod ## Builds a local binary for darwin/amd64.
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(APPLICATION)-darwin .

build-linux: $(SOURCES) go.mod ## Builds a local binary for linux/amd64.
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(APPLICATION)-linux .

install: ## Install the application.
	CGO_ENABLED=0 GOOS=$(OS) go install -ldflags "$(LDFLAGS)" .

test: ## Runs go test with default values.
	go test ./...

test-unit: ## Runs unit tests with race detector.
	go test -race ./...

test-integration: ## Runs integration tests.
	go test -tags=integration ./...

test-benchmark: ## Runs benchmarks.
	go test -bench=. ./...

clean: ## Remove binary files.
	rm -f $(APPLICATION)* 
