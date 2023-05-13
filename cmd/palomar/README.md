# Palomar

Palomar is an Elasticsearch/OpenSearch frontend and ATP (ActivityPub) repository crawler designed to provide search services for the Bluesky network.

## Prerequisites

- GoLang (version 1.x)
- Running instance of Elasticsearch or OpenSearch for indexing.
- Valid credentials for the Personal Data Store (PDS) you want to index against.

## Building

```
go build
```

## Configuration

Palomar uses environment variables for configuration. Here are the required ones:

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
