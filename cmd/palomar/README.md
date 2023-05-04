# thecloud (working title)

An elasticsearch frontend and ATP repo crawler meant to provide search services
for the bluesky network.

## Building

```
go build
```

## Setup

You will need a running elasticsearch instance (or cluster) for indexing, and
valid credentials for the PDS you wish to index against.

The following environment variables should be set:
- `ATP_BGS_HOST`
  - The url of the bluesky BGS, e.g. `https://bgs.staging.bsky.dev`
- `ELASTIC_HTTPS_FINGERPRINT` 
  - if using a self signed cert for your elasticsearch deployment, this must be set
- `ELASTIC_USERNAME`
  - elasticsearch username, defaults to `elastic`
- `ELASTIC_PASSWORD`
  - password for elasticsearch auth
- `ELASTIC_HOSTS`
  - comma separated list of elasticsearch endpoints 
- `READONLY`
  - To be set only if the instance should act as a readonly HTTP server (no indexing)

## Running

After ensuring the env is properly configured, run:

```
./thecloud run
```

## Indexing 
For now, there isnt an easy way to get updates from the PDS, so to keep the
index up to date you will periodcally need to scrape the data.

## API

### `/index/:did`
Index the content in the given users repo. Keeps track of the last repo update
and only fetches incremental changes

### `/search?q=QUERY`
Very simple case-insensitive search results from across the entire app.
