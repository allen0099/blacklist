BINARY      := blacklist
MODULE      := $(shell go list -m)
GO          := go
GOFILES     := $(shell find . -name '*.go' -not -path './vendor/*')

.PHONY: all build test vet fmt fmt-check vuln clean install-hooks help

## all: build the binary (default target)
all: build

## build: compile the binary
build:
	$(GO) build -o $(BINARY) .

## test: run all tests with the race detector
test:
	$(GO) test ./... -race

## test-coverage: run tests and print a coverage summary
test-coverage:
	$(GO) test ./... -race -coverprofile=coverage.out -covermode=atomic
	$(GO) tool cover -func=coverage.out

## vet: run go vet
vet:
	$(GO) vet ./...

## vuln: run govulncheck to scan for known vulnerabilities
vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...

## fmt: format all Go source files in-place
fmt:
	gofmt -w $(GOFILES)

## fmt-check: fail if any Go source files are not formatted
fmt-check:
	@UNFORMATTED=$$(gofmt -l $(GOFILES)); \
	if [ -n "$$UNFORMATTED" ]; then \
		echo "The following files are not gofmt-formatted:"; \
		echo "$$UNFORMATTED"; \
		echo "Run 'make fmt' to fix."; \
		exit 1; \
	fi
	@echo "All Go files are properly formatted."

## clean: remove build artifacts
clean:
	rm -f $(BINARY) coverage.out

## install-hooks: configure git to use the project's commit hooks
install-hooks:
	git config core.hooksPath .githooks
	chmod +x .githooks/pre-commit
	@echo "Git hooks installed (using .githooks/)."

## help: show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/  /'
