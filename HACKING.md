
## git repo contents

Run with, eg, `go run ./cmd/bigsky`):

- `cmd/bigsky`: Relay+indexer daemon
- `cmd/palomar`: search indexer and query servcie (OpenSearch)
- `cmd/gosky`: client CLI for talking to a PDS
- `cmd/lexgen`: codegen tool for lexicons (Lexicon JSON to Go package)
- `cmd/laputa`: partial PDS daemon (not usable or under development)
- `cmd/stress`: connects to local/default PDS and creates a ton of random posts
- `cmd/beemo`: slack bot for moderation reporting (Bluesky Moderation Observer)
- `cmd/fakermaker`: helper to generate fake accounts and content for testing
- `cmd/supercollider`: event stream load generation tool
- `cmd/sonar`: event stream monitoring tool
- `cmd/hepa`: auto-moderation rule engine service
- `cmd/rainbow`: firehose fanout service
- `gen`: dev tool to run CBOR type codegen

Packages:

- `api`: mostly output of lexgen (codegen) for lexicons: structs, CBOR marshaling. some higher-level code, and a PLC client (may rename)
    - `api/atproto`: generated types for `com.atproto` lexicon
    - `api/bsky`: generated types for `app.bsky` lexicon
- `atproto/crypto`: crytographic helpers (signing, key generation and serialization)
- `atproto/syntax`: string types and parsers for identifiers, datetimes, etc
- `atproto/identity`: DID and handle resolution
- `automod`: moderation and anti-spam rules engine
- `bgs`: relay server implementation for crawling, etc
- `carstore`: library for storing repo data in CAR files on disk, plus a metadata SQL db
- `events`: types, codegen CBOR helpers, and persistence for event feeds
- `indexer`: aggregator, handling like counts etc in SQL database
- `lex`: implements codegen for Lexicons (!)
- `models`: database types/models/schemas; shared in several places
- `mst`: merkle search tree implementation
- `notifs`: helpers for notification objects (hydration, etc)
- `pds`: PDS server implementation
- `plc`: implementation of a *fake* PLC server (not persisted), and a PLC client
- `repo`: implements atproto repo on top of a blockstore. CBOR types
- `repomgr`: wraps many repos with a single carstore backend. handles events, locking
- `search`: search server implementation
- `testing`: integration tests; testing helpers
- `util`: a few common definitions (may rename)
- `xrpc`: XRPC client (not server) helpers


## Jargon

- Relay: service which crawls/consumes content from "all" PDSs and re-broadcasts as a firehose
- BGS: Big Graph Service, previous name for what is now "Relay"
- PDS: Personal Data Server (or Service), which stores user atproto repositories and acts as a user agent in the network
- CLI: Command Line Tool
- CBOR: a binary serialization format, smilar to JSON
- PLC: "placeholder" DID provider, see <https://web.plc.directory>
- DID: Decentralized IDentifier, a flexible W3C specification for persistent identifiers in URI form (eg, `did:plc:abcd1234`)
- XRPC: atproto convention for HTTP GET and POST endpoints specified by namespaced Lexicon schemas
- CAR: simple file format for storing binary content-addressed blocks/blobs, sort of like .tar files
- CID: content identifier for binary blobs, basically a flexible encoding of hash values
- MST: Merkle Search Tree, a key/value map data structure using content addressed nodes


## Lexicon and CBOR code generation

`gen/main.go` has a list of types internal to packages in this repo which need CBOR helper codegen. If you edit those types, or update the listed types/packages, re-run codegen like:

    # make sure everything can build cleanly first
    make build

    # then generate
    go run ./gen

To run codegen for new or updated Lexicons, using lexgen, first place (or git checkout) the JSON lexicon files at `../atproto/`. Then, in *this* repository (indigo), run commands like:

    go run ./cmd/lexgen/ --package bsky --prefix app.bsky --outdir api/bsky ../atproto/lexicons/app/bsky/
    go run ./cmd/lexgen/ --package atproto --prefix com.atproto --outdir api/atproto ../atproto/lexicons/com/atproto/

You may want to delete all the codegen files before re-generating, to detect deleted files.

It can require some manual munging between the lexgen step and a later `go run ./gen` to make sure things compile at least temporarily; otherwise the `gen` will not run. In some cases, you might also need to add new types to `./gen/main.go`.

To generate server stubs and handlers, push them in a temporary directory first, then merge changes in to the actual PDS code:

    mkdir tmppds
    go run ./cmd/lexgen/ --package pds --gen-server --types-import com.atproto:github.com/bluesky-social/indigo/api/atproto --types-import app.bsky:github.com/bluesky-social/indigo/api/bsky --outdir tmppds --gen-handlers ../atproto/lexicons


## Tips and Tricks

When debugging websocket streams, the `websocat` tool (rust) can be helpful. CBOR binary is sort of mangled in to text by default. Eg:

    # consume repo events from PDS
    websocat ws://localhost:4989/events

    # consume repo events from Relay
    websocat ws://localhost:2470/events

Send the Relay a ding-dong:

    # tell Relay to consume from PDS
    http --json post localhost:2470/add-target host="localhost:4989"

Set the log level to be more verbose, using an env variable:

    GOLOG_LOG_LEVEL=info go run ./cmd/pds


## `gosky` basic usage

Running against local typescript PDS in `dev-env` mode:

	# as "alice" user
	go run ./cmd/gosky/ --pds http://localhost:2583 createSession alice.test hunter2 > bsky.auth

The `bsky.auth` file is the default place that `gosky` and other client commands will look for auth info.


## Integrated Development

Sometimes it is helpful to run a PLC, PDS, Relay, and other components, all locally on your laptop, across languages. This section describes one setup for this.

First, you need PostgreSQL running locally. This could be via docker, or the following commands assume some kind of debian/ubuntu setup with a postgres server package installed and running.

Create a user and databases for PLC+PDS:

    # use 'yksb' as weak default password for local-only dev
    sudo -u postgres createuser -P -s bsky

    sudo -u postgres createdb plc_dev -O bsky
    sudo -u postgres createdb pds_dev -O bsky

If you end up needing to wipe the databases:

    sudo -u postgres dropdb plc_dev
    sudo -u postgres dropdb pds_dev

Checkout the `did-method-plc` repo in on terminal and run:

    make run-dev-plc

Checkout the `atproto` repo in another terminal and run:

    make run-dev-pds

In this repo (indigo), start a Relay, in two separate terminals:

    make run-dev-relay

In a final terminal, run fakermaker to inject data into the system:

    # setup and create initial accounts; 100 by default
    mkdir data/fakermaker/
    export GOLOG_LOG_LEVEL=info
    go run ./cmd/fakermaker/ gen-accounts > data/fakermaker/accounts.json

    # create or update profiles for all the accounts
    go run ./cmd/fakermaker/ gen-profiles

    # create follow graph between accounts
    go run ./cmd/fakermaker/ gen-graph

    # create posts, including mentions and image uploads
    go run ./cmd/fakermaker/ gen-posts

    # create more interactions, such as likes, between accounts
    go run ./cmd/fakermaker/ gen-interactions

    # lastly, read-only queries, including timelines, notifications, and post threads
    go run ./cmd/fakermaker/ run-browsing
