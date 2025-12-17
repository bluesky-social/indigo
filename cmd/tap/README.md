`tap`: atproto sync utility
========================================

Tap simplifies AT sync by handling the firehose connection, verification, backfill, and filtering. Your application connects to a Tap and receives simple JSON events for only the repos and collections you care about, no need to worry about binary formats for validating cryptographic signatures.

Features and design decisions:

- verifies repo structure, MST integrity, and identity signatures
- automatic backfill: fetches full repo history from PDS when adding new repos
- filtered output: by DID list, by collection, or full network mode
- ordering guarantees: live events wait for historical backfill to complete
- delivery modes: WebSocket with acks, fire-and-forget, or webhook
- single Go binary
- SQLite or Postgres backend
- designed for moderate scale (millions of repos, 30k+ events/sec)

There is a convenient client library in Typescript for working with Tap at [@atproto/tap](https://github.com/bluesky-social/atproto/blob/main/packages/tap/README.md)

⚠️ Tap is still in beta and may have bugs. Please report any issues you encounter.

## Quick Start

```bash
# Run tap
go run ./cmd/tap run --disable-acks=true
# By default, the service uses SQLite at `./tap.db` and binds to port `:2480`.

# In a separate terminal, connect to receive events:
websocat ws://localhost:2480/channel

#  Add a repo to track
curl -X POST http://localhost:2480/repos/add \
  -H "Content-Type: application/json" \
  -d '{"dids": ["did:plc:ewvi7nxzyoun6zhxrhs64oiz"]}' # @atproto.com repo
```

Each repo will be backfilled from its PDS, then live events will stream as they arrive from the relay.

## HTTP API

- `GET /health`: returns `{"status":"ok"}`
- `WS /channel`: WebSocket endpoint to receive events
- `POST /repos/add`: add DIDs to track (triggers backfill of added repos)
- `POST /repos/remove`: remove DIDs (stops sync, deletes tracked repo metadata. does not delete buffered events in outbox)
- `GET /resolve/:did`: resolve a DID to its DID document
- `GET /info/:did`: get info about a tracked repo (repo state, repo rev, record count, error, retry count)
- `GET /stats/repo-count`: get total number of tracked repos
- `GET /stats/record-count`: get total number of tracked records
- `GET /stats/outbox-buffer`: get number of events in outbox buffer
- `GET /stats/resync-buffer`: get number of events in resync buffer
- `GET /stats/cursors`: get current firehose and list repos cursors

If more than one client connects to the WebSocket, events will be transparently sharded across all connected clients. There are no guarantees around sharding, events are delivered to an any available websocket consumer. Though the general deliverability guarantees (as described in *Per-Repo Ordering Rules*) hold across shards.

## Configuration

Environment variables or CLI flags:

- `TAP_DATABASE_URL`: database connection string, SQLite or PostgreSQL (default: `sqlite://./tap.db`)
- `TAP_MAX_DB_CONNS`: maximum number of database connections (default: `32`)
- `TAP_BIND`: HTTP server address (default: `:2480`)
- `TAP_PLC_URL`: PLC directory HTTP/HTTPS URL (default: `https://plc.directory`)
- `TAP_RELAY_URL`: AT Protocol relay HTTP/HTTPS URL (default: `https://relay1.us-east.bsky.network`)
- `TAP_FIREHOSE_PARALLELISM`: concurrent firehose event processors (default: `10`)
- `TAP_RESYNC_PARALLELISM`: concurrent resync workers (default: `5`)
- `TAP_OUTBOX_PARALLELISM`: concurrent outbox workers (default: `1`)
- `TAP_CURSOR_SAVE_INTERVAL`: how often to persist upstream firehose cursor (default: `1s`)
- `TAP_REPO_FETCH_TIMEOUT`: timeout for fetching repo CARs from PDS (default: `300s`)
- `TAP_IDENT_CACHE_SIZE`: size of in-process identity cache (default: `2000000`)
- `TAP_OUTBOX_CAPACITY`: rough size of outbox before back pressure is applied (default: `100000`)
- `TAP_FULL_NETWORK`: track all repos on the network (default: `false`)
- `TAP_SIGNAL_COLLECTION`: track all repos with at least one record in this collection (e.g. `app.bsky.actor.profile`)
- `TAP_COLLECTION_FILTERS`: comma-separated collection filters, wildcards accepted (e.g., `app.bsky.feed.post,app.bsky.graph.*`)
- `TAP_DISABLE_ACKS`: fire-and-forget mode, no client acks (default: `false`)
- `TAP_WEBHOOK_URL`: webhook URL for event delivery (disables WebSocket mode)
- `TAP_OUTBOX_ONLY`: run in outbox-only mode (no firehose, resync, or enumeration) (default: `false`)
- `TAP_ADMIN_PASSWORD`: Basic auth admin password required for all requests (if set)
- `TAP_RETRY_TIMEOUT`: timeout before retrying unacked events (default: `60s`)
- `TAP_LOG_LEVEL`: log verbosity (`debug`, `info`, `warn`, `error`, default: `info`)
- `TAP_METRICS_LISTEN`: address for metrics/pprof server (disabled if empty)

## Delivery Modes

Tap supports three delivery modes:

**WebSocket with acks** (default): Client sends acks each event once it has been processed/persisted. Ensures that no data is lost and client does not need to handle cursors. It's recommended to use a client library such as [@atproto/tap](https://github.com/bluesky-social/atproto/tree/main/packages/tap/README.md) when using this mode.

**Fire-and-forget**: Set `TAP_DISABLE_ACKS=true`. Events are sent and considered "acked" once the client receives them. Simpler but may result in data loss. Recommended for testing purposes or when data integrity is not critical.

**Webhook**: Set `TAP_WEBHOOK_URL=http://...`. Events are POSTed as JSON. Events considered "acked" once the webhook responds with a 200. Recommended for lower throughput serverless environments.


## Network Boundary Modes

Tap syncs a subset of repos in the network. It can operate in three modes for determining this network boundary.

**Dynamically Configured** (default):  Tap starts out tracking no repos. Specific repos can then by added via `/repos/add` and removed via `/repos/remove`.

**Collection Signal**: Set `TAP_SIGNAL_COLLECTION=com.example.nsid`. Track all repos that have at least one record in the specified collection. Many applications create a "declaration" or "profile" in a repo when that repo uses that application

**Full Network**: Set `TAP_FULL_NETWORK=true`. Enumerates and tracks all findable repos on the entire network. Resource-intensive and takes days/weeks to complete backfill.

## Collection Filtering

After narrowing down the network to a subset of repos, Tap can further filter records down to a specified set of collections. Filters apply to record events only. Identity events are always delivered for tracked repos.

If you are interested syncing all of a single record type, it is important to specify that collection as both the signal collection and the filter collection. For example: `TAP_SIGNAL_COLLECTION=com.example.nsid TAP_COLLECTION_FILTERS=com.example.nsid`

Collection filters use wildcards but only at the period breaks in NSIDs. For example:

`TAP_COLLECTION_FILTERS=app.bsky.feed.post,app.bsky.graph.*`


## Event Format

Events are delivered as JSON:

**Record events** (create, update, delete):

```json
{
  "id": 12345,
  "type": "record",
  "record": {
    "live": true, // true if a record was received over the firehose rather than backfill/resync
    "rev": "3kb3fge5lm32x",
    "did": "did:plc:abc123",
    "collection": "app.bsky.feed.post",
    "rkey": "3kb3fge5lm32x",
    "action": "create",
    "cid": "bafyreig...",
    "record": {
      "text": "Hello world!",
      "$type": "app.bsky.feed.post",
      "createdAt": "2024-10-07T12:00:00.000Z"
    }
  }
}
```

**Identity events** (handle or status changes):

```json
{
  "id": 12346,
  "type": "identity",
  "identity": {
    "did": "did:plc:abc123",
    "handle": "alice.bsky.social",
    "isActive": true,
    "status": "active"
  }
}
```

## Backfill 

When a repo is added (via `/repos/add`, full network mode, or collection discovery):

1. **Historical backfill**: Tap fetches the full repo from the account's PDS using `com.atproto.sync.getRepo`
2. **Live event buffering**: Any firehose events for this repo during backfill are held in memory
3. **Ordering guarantee**: Historical events (marked `live: false`) are delivered first
4. **Cutover**: After historical events complete, buffered live events are drained
5. **Live streaming**: New firehose events are delivered immediately (marked `live: true`)

This ensures your application receives a complete, ordered view of each repo without gaps or duplicates.

### Per-Repo Ordering Rules

Tap offloads cursor management and takes care of delivery guarantees. Events are delivered *at least once*. Events may be delivered more than once if Tap crashes and restarts before receiving an ack for a given event or if the event times out before being acked (default 10s).

There is no global ordering of events across repos. However Tap will ensure ordering within each repo and will avoid sending the next event until the previous event has completed processing.

Events for the same repo are delivered with strict ordering:

- **Live events** (`live: true`) are synchronization barriers - all prior events must complete before a live event can be sent, and the live event must complete (acked) before any subsequent events are sent
- **Historical events** (`live: false`, in the case of backfill/resyncs) can be sent concurrently with each other, but cannot be sent while a live event is in-flight

Example sequence: `H1, H2, L1, H3, H4, L2, H5`
- H1 and H2 sent concurrently
- Wait for H1 and H2 to complete, then send L1 (alone)
- Wait for L1 to complete, then send H3 and H4 concurrently
- Wait for H3 and H4 to complete, then send L2 (alone)
- Wait for L2 to complete, then send H5

This ensures live events act as ordering checkpoints while allowing historical backfill to run quickly.

## Authentication

If exposing Tap to the internet, you should set `TAP_ADMIN_PASSWORD` to require authentication for all API requests.

Tap uses HTTP Basic authentication with the username `admin`. Basic auth works by concatenating the username and password with a colon (`admin:yourpassword`), then base64-encoding the result. This is sent in the `Authorization` header as `Basic <encoded-value>`.

Example with curl:
```bash
curl -u admin:yourpassword http://localhost:2480/repos/add \
  -H "Content-Type: application/json" \
  -d '{"dids": ["did:plc:..."]}'
```

When using webhook mode, Tap sends the same Basic auth credentials to your webhook endpoint.

## Operations

Tap logs to stdout in JSON format. The firehose consumer automatically reconnects with exponential backoff on relay failures. Cursor position is saved periodically (default 1 second) and restored on restart.

SQLite is tuned for high write throughput: WAL mode, 10-second busy timeout, `synchronous=NORMAL`, 64MB cache, batched deletes. The outbox buffers up to 1M pending events in memory.

Resync is automatic: if a commit does not validate according to [Sync v1.1](https://github.com/bluesky-social/proposals/tree/main/0006-sync-iteration) semantics, the repo is marked `desyncrhonized` until it can be refetched from the authoritative PDS. Live events during resync are buffered and replayed after completion. Failures trigger exponential backoff (1 minute → 1 hour max).

Identity resolution uses a cached directory (24-hour TTL). DNS lookups are skipped for `*.bsky.social` handles. The cache warms up at startup and may cause a burst of PLC directory requests.

## Distribution & Deployment

Tap is distributed as a single Go binary and is easy to build and run.

**Build from source:**
```bash
go build -o tap ./cmd/tap
./tap run
```

**Docker:**

A pre-built Docker image is also available:
```bash
docker pull ghcr.io/bluesky-social/indigo/tap:latest
docker run -p 2480:2480 ghcr.io/bluesky-social/indigo/tap:latest
```

To persist data, mount a volume at `/data`:
```bash
docker run -p 2480:2480 -v ./data:/data ghcr.io/bluesky-social/indigo/tap:latest
```

The Dockerfile is included in the repo at `cmd/tap/Dockerfile` if you need to build your own image.
