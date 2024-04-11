#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

export SCYLLA_KEYSPACE="${SCYLLA_KEYSPACE:-}"
export SCYLLA_HOST="${SCYLLA_HOST:-}"

# Used by pagerank.
export FOLLOWS_FILE="/data/follows.csv"
export ACTORS_FILE="/data/actors.csv"
export OUTPUT_FILE="/data/pageranks.csv"
export EXPECTED_ACTOR_COUNT="5000000"
export RUST_LOG="info"

# Used by palomar.
export PAGERANK_FILE="${OUTPUT_FILE}"
export PALOMAR_INDEXING_RATE_LIMIT="10000"

function run_pagerank {
    # Check that the required environment variables are set.
    if [[ "${SCYLLA_KEYSPACE}" == "" ]]; then
        echo "SCYLLA_KEYSPACE is not set"
        exit 1
    fi

    if [[ "${SCYLLA_HOST}" == "" ]]; then
        echo "SCYLLA_HOST is not set"
        exit 1
    fi

    # Dump the tables to CSV files.
    rm --force "${FOLLOWS_FILE}"
    cqlsh \
        "--keyspace=${SCYLLA_KEYSPACE}" \
        "--request-timeout=1200" \
        ---execute "COPY follows (actor_did, subject_did) TO '${FOLLOWS_FILE}' WITH HEADER = FALSE;" \
        "${SCYLLA_HOST}"

    rm --force "${ACTORS_FILE}"
    cqlsh \
        "--keyspace=${SCYLLA_KEYSPACE}" \
        "--request-timeout=1200" \
        ---execute "COPY actors (did) TO '${ACTORS_FILE}' WITH HEADER = FALSE;" \
        "${SCYLLA_HOST}"

    # Run the pagerank file which reads in the table CSV files and outputs a CSV.
    /usr/local/bin/pagerank

    # Run palomar with the pagerank CSV file.
    /palomar run
}

while true; do
    run_pagerank
    sleep 24h
done
