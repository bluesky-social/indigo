
`collectiondir`: Directory of Accounts by Collection
====================================================

This is a small atproto microservice which maintains a directory of which accounts in the network (DIDs) have data (records) for which collections (NSIDs).

It primarily serves the `com.atproto.sync.listReposByCollection` API endpoint:

```
GET /xrpc/com.atproto.sync.listReposByCollection?collection=com.atproto.sync.listReposByCollection?collection=com.atproto.lexicon.schema&limit=3

{
    "repos": [
        { "did": "did:plc:4sm3vprfyl55ui3yhjd7w4po" },
        { "did": "did:plc:xhkqwjmxuo65vwbwuiz53qor" },
        { "did": "did:plc:w3aonw33w3mz3mwws34x5of6" }
    ],
    "cursor": "QQAAAEkAAAGVgFFLb2RpZDpwbGM6dzNhb253MzN3M216M213d3MzNHg1b2Y2AA=="
}
```

Features and design points:

- persists data in a local key/value database (pebble)
- consumes from the firehose to stay up to date with record creation
- can bootstrap the full network using `com.atproto.sync.listRepos` and `com.atproto.repo.describeRepo`
- single golang binary for easy deployment


## Analytics Endpoint

```
/v1/listCollections?c={}&cursor={}&limit={50<=limit<=1000}
```

`listCollections` returns JSON with a map of collection name to approximate number of dids implementing it.
With no `c` parameter it returns all known collections with cursor paging.
With up to 20 repeated `c` parameters it returns only those collections (no paging).
It may be the cached result of a computation, up to several minutes out of date.
```json
{"collections":{"gndr.app.feed.post": 123456789, "some collection": 42},
"cursor":"opaque text"}
```


## Database Schema

The primary database is (collection, seen time int64 milliseconds, did)

This allows for efficient cursor fetching of more dids for a collection.

e.g. A new service starts consuming the firehose for events it wants in collection `com.newservice.data.thing`,
it then calls the collection directory for a list of repos which may have already created data in this collection,
and does `getRepo` calls to those repo's PDSes to get prior data.
By the time it is done paging forward through the collection directory results and getting those repos,
it will have backfilled data and new data it has collected live off the firehose.
