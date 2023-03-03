
SHELL = /bin/bash
.SHELLFLAGS = -o pipefail -c

.PHONY: help
help: ## Print info about all commands
	@echo "Commands:"
	@echo
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "    \033[01;32m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: all
all: build

.PHONY: build
build: ## Build all executables
	go build ./cmd/gosky
	go build ./cmd/laputa
	go build ./cmd/bigsky
	go build ./cmd/beemo
	go build ./cmd/lexgen
	go build ./cmd/stress
	go build ./cmd/fakermaker

.PHONY: test
test: ## Run all tests
	go test ./...

.PHONY: coverage-html
coverage-html: ## Generate test coverage report and open in browser
	go test ./... -coverprofile=test-coverage.out
	go tool cover -html=test-coverage.out

.PHONY: lint
lint: ## Run style checks and verify syntax
	go vet -asmdecl -assign -atomic -bools -buildtag -cgocall -copylocks -httpresponse -loopclosure -lostcancel -nilfunc -printf -shift -stdmethods -structtag -tests -unmarshal -unreachable -unsafeptr -unusedresult ./...
	test -z $(gofmt -l ./...)

.PHONY: fmt
fmt: ## Run syntax re-formatting
	go fmt ./...
