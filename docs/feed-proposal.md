# Feed Structuring Proposal

Some thoughts on a new format for feeds.

## Motivation
The interface for requesting and getting back feeds is something that I feel is really at the core of what gander offers. The user should be able to choose what feeds they subscribe to, feeds should be first class objects, they should be able to be efficiently generated and consumed, and they should be able to trustlessly come from anywhere. 
There are a lot of changes we *could* make to the current structure, but I don't want to stray too far from where we are at right now.


```go
type Feed struct {
  Items []FeedItem
  Values map[Cid]Record
  ItemInfos map[Uri]ItemInfo
  ActorInfos map[Did]ActorInfo
}

type FeedItem struct {
  Uri string
  Replies []Uri
  ReplyTo Uri
  RepostedBy Did
}

type ItemInfo struct {
  Cid Cid
  Upvotes int
  Reposts int
  Replies int
  Author Did
}
```

The main idea here is not repeating ourselves, while still providing all the information the client might need.
With this structure too, the user could easily request *less* data, asking to
skip the inclusion of records older than X, or saying they are okay with stale
information in certain places for the sake of efficiency.
