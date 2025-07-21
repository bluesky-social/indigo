# Butterfly

A sync engine for atproto with an optional baked-in database.

## Example selectors

My data:

```json
{
  "select": [
    {
      "where": {"repo": "at://pfrazee.com"},
      "tag": "user"
    }
  ],
  "retain": {
    "user": {"*": "*"},
  }
}
```

A list of repos:

```json
{
  "select": [
    {"where": {"repo": "at://pfrazee.com"}, "tag": "user"},
    {"where": {"repo": "at://atproto.com"}, "tag": "user"},
    {"where": {"repo": "at://bsky.app"}, "tag": "user"}
  ],
  "retain": {
    "user": {"*": "*"},
  }
}
```

My personal network:

```json
{
  "select": [
    {
      "where": {"repo": "at://pfrazee.com"},
      "tag": "me"
    },
    {
      "where": {"repo": "me", "collection": "app.bsky.graph.follow", "attr": "subject"},
      "tag": "followed"
    },
    {
      "where": {"repo": "followed", "collection": "app.bsky.graph.follow", "attr": "subject"},
      "tag": "k2-followed"
    }
  ],
  "retain": {
    "me": {"*": "*"},
    "followed": : {"*": "*"},
    "k2-followed": {
      "app.bsky.actor.profile": "self",
      "app.bsky.graph.follow": "latest:3000",
      "app.bsky.feed.post": "latest:10",
      "app.bsky.feed.repost": "latest:10",
      "app.bsky.feed.like": "latest:100",
      "app.bsky.*": "latest:1000"
    }
  }
}
```

Recursive crawl from me:

```json
{
  "select": [
    {
      "where": {"repo": "at://pfrazee.com"},
      "tag": "user"
    },
    {
      "where": {"repo": "user", "collection": "app.bsky.graph.follow", "attr": "subject"},
      "tag": "user"
    }
  ],
  "retain": {
    "user": {"*": "*"},
  }
}
```

All users tracked by bluesky's collection index:

```json
{
  "select": [
    {
      "where": {
        "service": "bsky.network",
        "method": "com.atproto.sync.listReposByCollection",
        "params": {"collection": "app.bsky.actor.profile"},
        "attr": "repos.*.did",
        "pagination": {"param": "cursor", "attr": "cursor"}
      },
      "tag": "bluesky-user"
    }
  ],
  "retain": {
    "bluesky-user": {"app.bsky.*": "*"}
  }
}
```
