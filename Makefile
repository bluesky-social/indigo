SHELL = /bin/bash
.SHELLFLAGS = -o pipefail -c

# base path for Lexicon document tree (for lexgen)
LEXDIR?=../atproto/lexicons

# https://github.com/golang/go/wiki/LoopvarExperiment
export GOEXPERIMENT := loopvar

.PHONY: help
help: ## Print info about all commands
	@echo "Commands:"
	@echo
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "    \033[01;32m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build all executables
	go build ./cmd/goat
	go build ./cmd/gosky
	go build ./cmd/bigsky
	go build ./cmd/relay
	go build ./cmd/beemo
	go build ./cmd/lexgen
	go build ./cmd/stress
	go build ./cmd/fakermaker
	go build ./cmd/hepa
	go build ./cmd/supercollider
	go build -o ./sonar-cli ./cmd/sonar
	go build ./cmd/palomar

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
	go clean -testcache && go test -tags=localinterop ./...

.PHONY: test-search
test-search: ## Run tests, including local search indexing (requires services running)
	go clean -testcache && go test -tags=localsearch ./...

.PHONY: coverage-html
coverage-html: ## Generate test coverage report and open in browser
	go test ./... -coverpkg=./... -coverprofile=test-coverage.out
	go tool cover -html=test-coverage.out

.PHONY: lint
lint: ## Verify code style and run static checks
	go vet ./...
	test -z $(gofmt -l ./...)

.PHONY: fmt
fmt: ## Run syntax re-formatting (modify in place)
	go fmt ./...

.PHONY: check
check: ## Compile everything, checking syntax (does not output binaries)
	go build ./...

.PHONY: lexgen
lexgen: ## Run codegen tool for lexicons (lexicon JSON to Go packages)
	go run ./cmd/lexgen/ --build-file cmd/lexgen/gndr.json $(LEXDIR)

.PHONY: cborgen
cborgen: ## Run codegen tool for CBOR serialization
	go run ./gen

.env:
	if [ ! -f ".env" ]; then cp example.dev.env .env; fi

.PHONY: run-postgres
run-postgres: .env ## Runs a local postgres instance
	docker compose -f cmd/bigsky/docker-compose.yml up -d

.PHONY: run-dev-opensearch
run-dev-opensearch: .env ## Runs a local opensearch instance
	docker build -f cmd/palomar/Dockerfile.opensearch . -t opensearch-palomar
	docker run -p 9200:9200 -p 9600:9600 -e "discovery.type=single-node" -e "plugins.security.disabled=true" -e "OPENSEARCH_INITIAL_ADMIN_PASSWORD=0penSearch-Pal0mar" opensearch-palomar

.PHONY: run-dev-relay
run-dev-relay: .env ## Runs relay for local dev
	LOG_LEVEL=info go run ./cmd/relay --admin-password localdev serve

.PHONY: run-dev-ident
run-dev-ident: .env ## Runs 'bluepages' identity directory for local dev
	GOLOG_LOG_LEVEL=info go run ./cmd/bluepages serve

.PHONY: build-relay-image
build-relay-image: ## Builds relay docker image
	docker build -t relay -f cmd/relay/Dockerfile .

.PHONY: build-relay-admin-ui
build-relay-admin-ui: ## Build relay admin web UI
	cd  cmd/relay/relay-admin-ui; yarn install --frozen-lockfile; yarn build
	mkdir -p public
	cp -r cmd/relay/relay-admin-ui/dist/* public/

.PHONY: run-relay-image
run-relay-image:
	docker run -p 2470:2470 relay /relay serve --admin-password localdev
# --crawl-insecure-ws

.PHONY: run-dev-search
run-dev-search: .env ## Runs search daemon for local dev
	GOLOG_LOG_LEVEL=info go run ./cmd/palomar run

.PHONY: sonar-up
sonar-up: # Runs sonar docker container
	docker compose -f cmd/sonar/docker-compose.yml up --build -d || docker-compose -f cmd/sonar/docker-compose.yml up --build -d

.PHONY: sc-reload
sc-reload: # Reloads supercollider
	go run cmd/supercollider/main.go \
		reload \
		--port 6125 --total-events 2000000 \
		--hostname alpha.supercollider.jazco.io \
		--key-file out/alpha.pem \
		--output-file out/alpha_in.cbor

.PHONY: sc-fire
sc-fire: # Fires supercollider
	go run cmd/supercollider/main.go \
		fire \
		--port 6125 --events-per-second 10000 \
		--hostname alpha.supercollider.jazco.io \
		--key-file out/alpha.pem \
		--input-file out/alpha_in.cbor

.PHONY: run-netsync
run-netsync: .env ## Runs netsync for local dev
	go run ./cmd/netsync --checkout-limit 100 --worker-count 100 --out-dir ../netsync-out

SCYLLA_VERSION := latest
SCYLLA_CPU := 0
SCYLLA_NODES := 127.0.0.1:9042

.PHONY: run-scylla
run-scylla:
	@echo "==> Running test instance of Scylla $(SCYLLA_VERSION)"
	@docker pull scylladb/scylla:$(SCYLLA_VERSION)
	@docker run --name scylla -p 9042:9042 --cpuset-cpus=$(SCYLLA_CPU) --memory 1G --rm -d scylladb/scylla:$(SCYLLA_VERSION)
	@until docker exec scylla cqlsh -e "DESCRIBE SCHEMA"; do sleep 2; done

.PHONY: stop-scylla
stop-scylla:
	@echo "==> Stopping test instance of Scylla $(SCYLLA_VERSION)"
	@docker stop scylla

.PHONY: docker-build-plc

docker-build-plc: ## Build the plc Docker image
	docker build -t gander-plc ./plc

.PHONY: docker-run-plc

docker-run-plc: ## Run the plc service in Docker
	docker run --rm -it -p 2583:2583 --name gander-plc gander-plc

.PHONY: docker-build-pds-one

docker-build-pds-one: ## Build the pds-one Docker image
	docker build -t gander-pds-one ./pds

.PHONY: docker-run-pds-one

docker-run-pds-one: ## Run the pds-one service in Docker
	docker run --rm -it -p 2582:2582 --name gander-pds-one gander-pds-one

.PHONY: docker-build-bgs

docker-build-bgs: ## Build the bgs Docker image
	docker build -t gander-bgs ./cmd/bigsky

.PHONY: docker-run-bgs

docker-run-bgs: ## Run the bgs service in Docker
	docker run --rm -it -p 2470:2470 --name gander-bgs gander-bgs

# Optionally, add appview targets if a Dockerfile is present
.PHONY: docker-build-appview

docker-build-appview: ## Build the appview Docker image (if Dockerfile exists)
	@if [ -f ./appview/Dockerfile ]; then \
		docker build -t gander-appview ./appview; \
	else \
		echo "No Dockerfile found for appview. Skipping."; \
	fi

.PHONY: docker-run-appview

docker-run-appview: ## Run the appview service in Docker (if image exists)
	@if docker image inspect gander-appview > /dev/null 2>&1; then \
		docker run --rm -it -p 2584:2584 --name gander-appview gander-appview; \
	else \
		echo "No image found for appview. Build it first or add a Dockerfile."; \
	fi
