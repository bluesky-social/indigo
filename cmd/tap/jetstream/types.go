package jetstream

import (
	comatproto "github.com/bluesky-social/indigo/api/atproto"
)

// Jetstream events copied from the project
type JetstreamEvent struct {
	Did      string                                  `json:"did"`
	TimeUS   int64                                   `json:"time_us"`
	Kind     string                                  `json:"kind,omitempty"`
	Commit   *Commit                                 `json:"commit,omitempty"`
	Account  *comatproto.SyncSubscribeRepos_Account  `json:"account,omitempty"`
	Identity *comatproto.SyncSubscribeRepos_Identity `json:"identity,omitempty"`
	Sync     *comatproto.SyncSubscribeRepos_Sync     `json:"sync,omitempty"`
}

type Commit struct {
	Rev        string         `json:"rev,omitempty"`
	Operation  string         `json:"operation,omitempty"`
	Collection string         `json:"collection,omitempty"`
	RKey       string         `json:"rkey,omitempty"`
	Record     map[string]any `json:"record,omitempty"`
	CID        string         `json:"cid,omitempty"`
}

var (
	EventKindCommit   = "commit"
	EventKindAccount  = "account"
	EventKindIdentity = "identity"
	EventKindSync     = "sync"

	CommitOperationCreate = "create"
	CommitOperationUpdate = "update"
	CommitOperationDelete = "delete"
)
