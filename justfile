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

# Builds and a go executable
r CMD *ARGS:
    go run ./cmd/{{CMD}} {{ARGS}}

# Builds and runs a go executable with the race detector enabled
run CMD *ARGS:
    go run -race ./cmd/{{CMD}} {{ARGS}}

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

# Generates protobuf sources
build-protos:
    #!/usr/bin/env bash
    set +x

    pushd pkg > /dev/null

    # generate, then clean up the protos for all connect services
    for PKG in prototypes; do
        pushd $PKG > /dev/null

        buf lint
        buf generate

        if [[ -e "${PKG}connect/${PKG}.connect.go" ]]; then
            mv "${PKG}connect/${PKG}.connect.go" .
            rmdir "${PKG}connect"

            # remove unnecessary/broken import
            sed -i.bak "/^\t${PKG} \"/d" "${PKG}.connect.go"

            # remove qualified names from that removed import, but ignore ones
            # that are preceeded by a slash character (i.e. "/agent.Service/Ping")
            sed -i.bak "s/\([^/]\)${PKG}\./\1/g; s/^${PKG}\.//" "${PKG}.connect.go"

            # move the generated code to our top level package
            sed -i.bak "s/package ${PKG}connect/package ${PKG}/" "${PKG}.connect.go"

            # remove package qualification, since it's all in our top level package
            sed -i.bak 's/__\.//g' "${PKG}.connect.go"

            # clean up .bak file. This was required to make sed in-place flag work the same on mac and linux
            rm "${PKG}.connect.go.bak"

            # run go fmt
            go fmt "${PKG}.connect.go" >/dev/null
        fi

        popd > /dev/null
    done

    go fmt ./...
    popd > /dev/null
