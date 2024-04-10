# Palomar

Palomar is a backend search service for atproto, specifically the `bsky.app` post and profile record types. It works by consuming a repo event stream ("firehose") and updating an OpenSearch cluster (fork of Elasticsearch) with docs.

Almost all the code for this service is actually in the `search/` directory at the top of this repo.

In September 2023, this service was substantially re-written. It no longer stores records in a local database, returns only "skeleton" results (list of ATURIs or DIDs) via the HTTP API, and defines index mappings.


## Query String Syntax

Currently only a simple query string syntax is supported. Double-quotes can surround phrases, `-` prefix negates a single keyword, and the following initial filters are supported:

- `from:<handle>` will filter to results from that account, based on current (cached) identity resolution
- entire DIDs as an un-quoted keyword will result in filtering to results from that account


## Configuration

Palomar uses environment variables for configuration.

- `ATP_RELAY_HOST`: URL of firehose to subscribe to, either global Relay or individual PDS (default: `wss://bsky.network`)
- `ATP_PLC_HOST`: PLC directory for identity lookups (default: `https://plc.directory`)
- `DATABASE_URL`: connection string for database to persist firehose cursor subscription state
- `PALOMAR_BIND`: IP/port to have HTTP API listen on (default: `:3999`)
- `ES_USERNAME`: Elasticsearch username (default: `admin`)
- `ES_PASSWORD`: Password for Elasticsearch authentication
- `ES_CERT_FILE`: Optional, for TLS connections
- `ES_HOSTS`: Comma-separated list of Elasticsearch endpoints
- `ES_POST_INDEX`: name of index for post docs (default: `palomar_post`)
- `ES_PROFILE_INDEX`: name of index for profile docs (default: `palomar_profile`)
- `PALOMAR_READONLY`: Set this if the instance should act as a readonly HTTP server (no indexing)

## HTTP API

### Query Posts: `/xrpc/app.bsky.unspecced.searchPostsSkeleton`

HTTP Query Params:

- `q`: query string, required
- `limit`: integer, default 25
- `cursor`: string, for partial pagination (uses offset, not a scroll)

Response:

- `posts`: array of AT-URI strings
- `hits_total`: integer; optional number of search hits (may not be populated for large result sets, eg over 10k hits)
- `cursor`: string; optionally included if there are more results that can be paginated

### Query Profiles: `/xrpc/app.bsky.unspecced.searchActorsSkeleton`

HTTP Query Params:

- `q`: query string, required
- `limit`: integer, default 25
- `cursor`: string, for partial pagination (uses offset, not a scroll)
- `typeahead`: boolean, for typeahead behavior (vs. full search)

Response:

- `actors`: array of AT-URI strings
- `hits_total`: integer; optional number of search hits (may not be populated for large result sets, eg over 10k hits)
- `cursor`: string; optionally included if there are more results that can be paginated

## Development Quickstart

Run an ephemeral opensearch instance on local port 9200, with SSL disabled, and the `analysis-icu` and `analysis-kuromoji` plugins installed, using docker:

    docker build -f Dockerfile.opensearch . -t opensearch-palomar

	# in any non-development system, obviously change this default password
    docker run -p 9200:9200 -p 9600:9600 -e "discovery.type=single-node" -e "plugins.security.disabled=true" -e OPENSEARCH_INITIAL_ADMIN_PASSWORD=0penSearch-Pal0mar opensearch-palomar

See [README.opensearch.md]() for more Opensearch operational tips.

From the top level of the repository:

    # run combined indexing and search service
    make run-dev-search

    # run just the search service
    READONLY=true make run-dev-search

You'll need to get some content in to the index. An easy way to do this is to have palomar consume from the public production firehose.

You can run test queries from the top level of the repository:

    go run ./cmd/palomar search-post "hello"
    go run ./cmd/palomar search-profile "hello"
    go run ./cmd/palomar search-profile -typeahead "h"

For more commands and args:

    go run ./cmd/palomar --help
