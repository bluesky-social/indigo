# Collection Directory

Maintain a directory of which repos use which collections of records.

e.g. "app.bsky.feed.post" is used by did:alice did:bob

Firehose consumer and crawler of PDS via listRepos and describeRepo.

The primary query is:

```
/v1/getDidsForCollection?collection={}&cursor={}
```

It returns JSON:

```json
{"dids":["did:A", "..."],
"cursor":"opaque text"}
```

query parameter `collection` may be repeated up to 10 times. They must always be sent in the same order or the cursor will break.

If multiple collections are specified, the result stream is not guaranteed to be de-duplicated on Did and Dids may be repeated.
(A merge window is used so that the service is _likely_ to not send duplicate Dids.)


### Analytics queries

```
/v1/listCollections?c={}&cursor={}&limit={50<=limit<=1000}
```

`listCollections` returns JSON with a map of collection name to approximate number of dids implementing it.
With no `c` parameter it returns all known collections with cursor paging.
With up to 20 repeated `c` parameters it returns only those collections (no paging).
It may be the cached result of a computation, up to several minutes out of date.
```json
{"collections":{"app.bsky.feed.post": 123456789, "some collection": 42},
"cursor":"opaque text"}
```


## Design

### Schema

The primary database is (collection, seen time int64 milliseconds, did)

This allows for efficient cursor fetching of more dids for a collection.

e.g. A new service starts consuming the firehose for events it wants in collection `com.newservice.data.thing`,
it then calls the collection directory for a list of repos which may have already created data in this collection,
and does `getRepo` calls to those repo's PDSes to get prior data.
By the time it is done paging forward through the collection directory results and getting those repos,
it will have backfilled data and new data it has collected live off the firehose.
