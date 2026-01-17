set shell := ["bash", "-cu"]

# Lints and runs all tests with the race detector enabled
default: lint test

# Ensures that all tools required for local development are installed. Before running, ensure you have go installed as well as protoc
install-tools:
    go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.7.2

# Stands up local development dependencies in docker
up:
    #!/usr/bin/env bash
    docker compose up -d

    if ! fdbcli -C foundation.cluster --exec status --timeout 1 ; then
        if ! fdbcli -C foundation.cluster --exec "configure new single ssd-redwood-1 ; status" --timeout 10 ; then
            echo "Unable to configure new FDB cluster."
            exit 1
        fi
    fi

    echo "development environment is ready"

# Tears down the local development dependencies
down:
    docker compose down --remove-orphans

# Lints the code
lint ARGS="./cmd/cask/... ./pkg/... ./internal/...":
    golangci-lint run --timeout 1m {{ARGS}}

# Builds and runs the given Go executable
r CMD *ARGS:
    go run ./cmd/{{CMD}} {{ARGS}}

# Builds and runs the given Go executable with the race detector enabled
run CMD *ARGS:
    just r -race {{CMD}} {{ARGS}}

# Runs the tests
t *ARGS="./cmd/cask/... ./internal/... ./pkg/...":
    go test -count=1 -covermode=atomic -coverprofile=test-coverage.out {{ARGS}}

# Runs the tests with the race detector enabled
test *ARGS="./cmd/cask/... ./internal/... ./pkg/...":
    just t -race {{ARGS}}

# run `just test` first, then run this to view test coverage
cover:
    go tool cover -html coverage.out

# Connects to the local foundationdb developement server
fdbcli:
    fdbcli -C foundation.cluster
