`nexus`: atproto sync utility
========================================

Nexus is a single-tenant service that subscribes to an atproto relay and outputs filtered, verified events for a subset of repos.

Nexus simplifies firehose consumption by handling verification, backfill, and filtering. Your application connects to nexus and receives simple JSON events for only the repos and collections you care about. Historical data for configured repos is automatically fetched from PDSs and delivered before live events begin.

Features and design decisions:

- verifies repo structure, MST integrity, and identity signatures
- automatic backfill: fetches full repo history from PDS when adding new repos
- filtered output: by DID list, by collection, or full network mode
- ordering guarantees: live events wait for historical backfill to complete
- delivery modes: WebSocket with acks, fire-and-forget, or webhook
- single golang binary, SQLite backend
- designed for moderate scale (thousands of repos, 10k+ events/sec)

This tool is useful for building applications that need to track specific accounts or collections without dealing with the complexity of repo verification and backfill orchestration.

⚠️ This tool is still in beta and may have bugs. Please report any issues you encounter.

## Quick Start

```bash
# Run nexus
go run ./cmd/nexus --disable-acks=true
# By default, the service uses SQLite at `./nexus.db` and binds to port `:8080`.

# In a separate terminal, connect to receive events:
websocat ws://localhost:8080/channel

#  Add a repo to track
curl -X POST http://localhost:8080/add-repos \
  -H "Content-Type: application/json" \
  -d '{"dids": ["did:plc:z72i7hdynmk6r22z27h6tvur"]}' # @bsky.app repo
```

Each repo will be backfilled from its PDS, then live events will stream as they arrive from the relay.

## HTTP API

- `GET /health`: returns `{"status":"ok"}`
- `POST /add-repos`: add DIDs to track (triggers backfill of added repos)
- `POST /remove-repos`: remove DIDs (stops sync, deletes data. does not delete buffered events in outbox)
- `GET /channel`: WebSocket endpoint to receive events

If more than one client connects to the WebSocket, events will be transparently sharded across all connected clients.

## Configuration

Environment variables or CLI flags:

- `NEXUS_DB_PATH`: path to SQLite database file (default: `./nexus.db`)
- `NEXUS_RELAY_URL`: atproto relay URL (default: `https://relay1.us-east.bsky.network`)
- `NEXUS_BIND`: HTTP server address (default: `:8080`)
- `NEXUS_FIREHOSE_PARALLELISM`: concurrent firehose event processors (default: `10`)
- `NEXUS_RESYNC_PARALLELISM`: concurrent resync workers (default: `5`)
- `NEXUS_OUTBOX_PARALLELISM`: concurrent outbox workers (default: `5`)
- `NEXUS_CURSOR_SAVE_INTERVAL`: how often to save cursor (default: `5s`, set to `0` to disable)
- `NEXUS_REPO_FETCH_TIMEOUT`: timeout for fetching repo CARs from PDS (Go duration syntax, e.g. `180s`)
- `NEXUS_FULL_NETWORK_MODE`: track all repos on the network (default: `false`)
- `NEXUS_SIGNAL_COLLECTION`: track all repos with at least one record in this collection (e.g. `app.bsky.actor.profile`)
- `NEXUS_COLLECTION_FILTERS`: comma-separated collection filters, wildcards accepted (e.g., `app.bsky.feed.post,app.bsky.graph.*`)
- `NEXUS_DISABLE_ACKS`: fire-and-forget mode, no client acks (default: `false`)
- `NEXUS_WEBHOOK_URL`: webhook URL for event delivery (disables WebSocket mode)
- `NEXUS_OUTBOX_ONLY`: run in outbox-only mode (no firehose, resync, or enumeration) (default: `false`)
- `NEXUS_LOG_LEVEL`: log verbosity (`debug`, `info`, `warn`, `error`, default: `info`)

## Delivery Modes

Nexus supports three delivery modes:

**WebSocket with acks** (default): Client sends acks each event once it has been processed/persisted. Ensures that no data is lost and client does not need to handle cursors. It's recommended to use a client library such as (@TODO) when using this mode.

**Fire-and-forget**: Set `NEXUS_DISABLE_ACKS=true`. Events are sent and considered "acked" once the client receives them. Simpler but may result in data loss. Recommended for testing purposes or when data integrity is not critical.

**Webhook**: Set `NEXUS_WEBHOOK_URL=http://...`. Events are POSTed as JSON. Events considered "acked" once the webhook responds with a 200. Recommended for lower throughput serverless environments.


## Network Boundary Modes

Nexus syncs a subset of repos in the network. It can operate in three modes for determining this network boundary.

**Dynamically Configured** (default):  Nexus starts out tracking no repos. Specific repos can then by added via `/add-repos` and removed via `/remove-repos`.

**Collection Signal**: Set `NEXUS_SIGNAL_COLLECTION=com.example.nsid`. Track all repos that have at least one record in the specified collection. Many applications create a "declaration" or "profile" in a repo when that repo uses that application

**Full Network**: Set `NEXUS_FULL_NETWORK_MODE=true`. Enumerates and tracks all findable repos on the entire network. Resource-intensive and takes days/weeks to complete backfill.

## Event Format

Events are delivered as JSON:

**Record events** (create, update, delete):

```json
{
  "id": 12345,
  "type": "record",
  "record": {
    "did": "did:plc:abc123",
    "collection": "app.bsky.feed.post",
    "rkey": "3kb3fge5lm32x",
    "action": "create",
    "cid": "bafyreig...",
    "record": {
      "text": "Hello world!",
      "$type": "app.bsky.feed.post",
      "createdAt": "2024-10-07T12:00:00.000Z"
    },
    "live": true
  }
}
```

**User events** (handle or status changes):

```json
{
  "id": 12346,
  "type": "user",
  "user": {
    "did": "did:plc:abc123",
    "handle": "alice.bsky.social",
    "isActive": true,
    "status": "active"
  }
}
```

## Backfill

When a repo is added (via `/add-repos`, full network mode, or collection discovery):

1. **Historical backfill**: Nexus fetches the full repo from the account's PDS using `com.atproto.sync.getRepo`
2. **Live event buffering**: Any firehose events for this repo during backfill are held in memory
3. **Ordering guarantee**: Historical events (marked `live: false`) are delivered first
4. **Cutover**: After historical events complete, buffered live events are drained
5. **Live streaming**: New firehose events are delivered immediately (marked `live: true`)

This ensures your application receives a complete, ordered view of each repo without gaps or duplicates.

### Per-Repo Ordering Rules

Nexus offloads cursor management and takes care of delivery guarantees. Events are delivered *at least once*. Events may be delivered more than once if Nexus crashes and restarts before receiving an ack for a given event or if the event times out before being acked (default 10s).

There is no global ordering of events across repos. However Nexus will ensure ordering within each repo and will avoid sending the next event until the previous event has completed processing.

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

## Collection Filtering

Collection filters use wildcards but only at the period breaks in NSIDs. For example:

`NEXUS_COLLECTION_FILTERS=app.bsky.feed.post,app.bsky.graph.*`

Filters apply to record events only. User events are always delivered for tracked repos.

## Operations

Nexus logs to stdout in JSON format. The firehose consumer automatically reconnects with exponential backoff on relay failures. Cursor position is saved periodically (default 5 seconds) and restored on restart.

SQLite is tuned for high write throughput: WAL mode, 10-second busy timeout, `synchronous=NORMAL`, 64MB cache, batched deletes. The outbox buffers up to 1M pending events in memory.

Resync is automatic: if a commit does not validate according to [Sync v1.1](https://github.com/bluesky-social/proposals/tree/main/0006-sync-iteration) semantics, the repo is marked `desynced` until it can be refetched from the authoritative PDS. Live events during resync are buffered and replayed after completion. Failures trigger exponential backoff (1 minute → 1 hour max).

Identity resolution uses a cached directory (24-hour TTL). DNS lookups are skipped for `*.bsky.social` handles. The cache warms up at startup and may cause a burst of PLC directory requests.
