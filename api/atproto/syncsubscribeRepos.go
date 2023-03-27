package atproto

import "github.com/ipfs/go-cid"

// schema: com.atproto.sync.subscribeRepos

func init() {
}

type SyncSubscribeRepos_RepoOp struct {
	LexiconTypeID string  `json:"$type,omitempty"`
	Action        string  `json:"action" cborgen:"action"`
	Cid           cid.Cid `json:"cid" cborgen:"cid"`
	Path          string  `json:"path" cborgen:"path"`
}
