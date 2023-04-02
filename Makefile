
SHELL = /bin/bash
.SHELLFLAGS = -o pipefail -c

# base path for Lexicon document tree (for lexgen)
LEXDIR?=../atproto/lexicons

.PHONY: help
help: ## Print info about all commands
	@echo "Commands:"
	@echo
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "    \033[01;32m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build all executables
	go build ./cmd/gosky
	go build ./cmd/laputa
	go build ./cmd/bigsky
	go build ./cmd/beemo
	go build ./cmd/lexgen
	go build ./cmd/stress
	go build ./cmd/fakermaker
	go build ./cmd/labelmaker

.PHONY: all
all: build

.PHONY: test
test: ## Run tests
	go test ./...

.PHONY: test-short
test-short: ## Run tests, skipping slower integration tests
	go test -test.short ./...

.PHONY: test-interop
test-interop: ## Run tests, including local interop (requires services running)
	go test -tags=localinterop ./...

.PHONY: coverage-html
coverage-html: ## Generate test coverage report and open in browser
	go test ./... -coverpkg=./... -coverprofile=test-coverage.out
	go tool cover -html=test-coverage.out

.PHONY: lint
lint: ## Verify code style and run static checks
	go vet -asmdecl -assign -atomic -bools -buildtag -cgocall -copylocks -httpresponse -loopclosure -lostcancel -nilfunc -printf -shift -stdmethods -structtag -tests -unmarshal -unreachable -unsafeptr -unusedresult ./...
	test -z $(gofmt -l ./...)

.PHONY: fmt
fmt: ## Run syntax re-formatting (modify in place)
	go fmt ./...

.PHONY: check
check: ## Compile everything, checking syntax (does not output binaries)
	go build ./...

.PHONY: lexgen
lexgen: ## Run syntax re-formatting (modify in place)
	go run ./cmd/lexgen/ --package bsky --prefix app.bsky --outdir api/bsky $(LEXDIR)
	go run ./cmd/lexgen/ --package atproto --prefix com.atproto --outdir api/atproto $(LEXDIR)

.env:
	if [ ! -f ".env" ]; then cp example.dev.env .env; fi

.PHONY: run-dev-bgs
run-dev-bgs: .env ## Runs 'bigsky' BGS for local dev
	GOLOG_LOG_LEVEL=info go run ./cmd/bigsky --crawl-insecure-ws

.PHONY: run-dev-labelmaker
run-dev-labelmaker: .env ## Runs labelmaker for local dev
	GOLOG_LOG_LEVEL=info go run ./cmd/labelmaker --subscribe-insecure-ws
