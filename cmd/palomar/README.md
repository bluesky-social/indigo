# Palomar

Palomar is a backend search service for atproto, specifically the `bsky.app` post and profile record types. It works by consuming a repo event stream ("firehose") and upating an Opensearch/Elasticsearch cluster with docs.

Almost all the code for this service is actually in the `search/` directory at the top of this repo.

## API

### `/search/posts?q=QUERY`

### `/search/profiles?q=QUERY`

### `/search/profiles-typeahead?q=QUERY`


## Development Quickstart

Run an ephemeral opensearch instance on local port 9200, with SSL disabled, and the `analysis-icu` plugin installed, using docker:

    docker build -f Dockerfile.opensearch . -t opensearch-palomar
    docker run -p 9200:9200 -p 9600:9600 -e "discovery.type=single-node" -e "plugins.security.disabled=true" opensearch-palomar

In this directory, use HTTPie to create indices:

    #http --verify no put https://admin:admin@localhost:9200/palomar_post < post_schema.json
    #http --verify no put https://admin:admin@localhost:9200/palomar_profile < profile_schema.json
    http put http://admin:admin@localhost:9200/palomar_post < post_schema.json
    http put http://admin:admin@localhost:9200/palomar_profile < profile_schema.json

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

## Configuration

Palomar uses environment variables for configuration.

- `ATP_BGS_HOST`: URL of the Bluesky BGS (e.g., `https://bgs.staging.bsky.dev`).
- `ELASTIC_HTTPS_FINGERPRINT`: Required if using a self-signed cert for your Elasticsearch deployment.
- `ELASTIC_USERNAME`: Elasticsearch username (default: `admin`).
- `ELASTIC_PASSWORD`: Password for Elasticsearch authentication.
- `ELASTIC_HOSTS`: Comma-separated list of Elasticsearch endpoints.
- `READONLY`: Set this if the instance should act as a readonly HTTP server (no indexing).

## Running the Application

Once the environment variables are set properly, you can start Palomar by running:

```
./palomar run
```

## Indexing

For now, there isnt an easy way to get updates from the PDS, so to keep the
index up to date you will periodcally need to scrape the data.

## API

### `/index/:did`

Indexes the content in the given user's repository. It keeps track of the last repository update and only fetches incremental changes.

### `/search?q=QUERY`

Performs a simple, case-insensitive search across the entire application.
