APP_NAME ?= sembr
GO_MAIN ?= .
MAKEFLAGS := --jobs=$(shell nproc)

.PHONY: all build run clean lint update help

all: build

build:
	goreleaser build --clean --snapshot --single-target

test:
	go test -short -race ./...
	go test ./...

lint: mod-tidy vet staticcheck golangci-lint fix govulncheck goreleaser-check

lint-fix:
	go mod tidy
	golangci-lint run --fix
	go fix -fix ./...
	go run ./cmd/sembr -fix
	$(MAKE) lint

mod-tidy:
	go mod tidy -diff

sembr:
	go run ./cmd/sembr

vet:
	go vet ./...

golangci-lint:
	golangci-lint config verify
	golangci-lint run

staticcheck:
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...

fix:
	go fix -diff ./...

govulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

goreleaser-check:
	goreleaser check

update:
	go get -v -u ./...
	go mod tidy

clean:
	rm -rf $(APP_NAME)

help:
	@echo "make           # Build $(APP_NAME) app"
	@echo "make lint-fix  # Run linters and fix issues"
	@echo "make lint      # Run linters and fix issues"
	@echo "make update    # Update Go dependencies"
	@echo "make clean     # Remove built app"
	@echo "APP_NAME=?     # Set app binary name"
