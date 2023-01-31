
.PHONY: help
help: ## Print info about all commands
	@echo "Commands:"
	@echo
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "    \033[01;32m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build all executables
	go build ./...

.PHONY: test
test: build ## Run all tests
	go test ./...

.PHONY: lint
lint: ## Run style checks and verify syntax
	go install github.com/kisielk/errcheck@latest
	errcheck ./...
	go vet -asmdecl -assign -atomic -bools -buildtag -cgocall -copylocks -httpresponse -loopclosure -lostcancel -nilfunc -printf -shift -stdmethods -structtag -tests -unmarshal -unreachable -unsafeptr -unusedresult ./...
	test -z $(gofmt -l ./...)
	# TODO: golangci-lint

.PHONY: fmt
fmt: ## Run syntax re-formatting
	go fmt ./...
