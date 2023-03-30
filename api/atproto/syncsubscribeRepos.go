package atproto

import (
	"github.com/bluesky-social/indigo/lex/util"
)

// schema: com.atproto.sync.subscribeRepos

func init() {
}

type SyncSubscribeRepos_Commit struct {
	LexiconTypeID string                       `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Blobs         []util.LexLink               `json:"blobs" cborgen:"blobs"`
	Blocks        util.LexBytes                `json:"blocks" cborgen:"blocks"`
	Commit        util.LexLink                 `json:"commit" cborgen:"commit"`
	Ops           []*SyncSubscribeRepos_RepoOp `json:"ops" cborgen:"ops"`
	Prev          *util.LexLink                `json:"prev" cborgen:"prev"`
	Rebase        bool                         `json:"rebase" cborgen:"rebase"`
	Repo          string                       `json:"repo" cborgen:"repo"`
	Seq           int64                        `json:"seq" cborgen:"seq"`
	Time          string                       `json:"time" cborgen:"time"`
	TooBig        bool                         `json:"tooBig" cborgen:"tooBig"`
}

type SyncSubscribeRepos_Handle struct {
	LexiconTypeID string `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Did           string `json:"did" cborgen:"did"`
	Handle        string `json:"handle" cborgen:"handle"`
	Seq           int64  `json:"seq" cborgen:"seq"`
	Time          string `json:"time" cborgen:"time"`
}

type SyncSubscribeRepos_Info struct {
	LexiconTypeID string  `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Message       *string `json:"message,omitempty" cborgen:"message"`
	Name          string  `json:"name" cborgen:"name"`
}

type SyncSubscribeRepos_Migrate struct {
	LexiconTypeID string  `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Did           string  `json:"did" cborgen:"did"`
	MigrateTo     *string `json:"migrateTo" cborgen:"migrateTo"`
	Seq           int64   `json:"seq" cborgen:"seq"`
	Time          string  `json:"time" cborgen:"time"`
}

type SyncSubscribeRepos_RepoOp struct {
	LexiconTypeID string        `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Action        string        `json:"action" cborgen:"action"`
	Cid           *util.LexLink `json:"cid" cborgen:"cid"`
	Path          string        `json:"path" cborgen:"path"`
}

type SyncSubscribeRepos_Tombstone struct {
	LexiconTypeID string `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Did           string `json:"did" cborgen:"did"`
	Seq           int64  `json:"seq" cborgen:"seq"`
	Time          string `json:"time" cborgen:"time"`
}
