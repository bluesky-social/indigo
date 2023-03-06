package atproto

// schema: com.atproto.sync.subscribeAllRepos

func init() {
}

type SyncSubscribeAllRepos_RepoOp struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Action        string `json:"action" cborgen:"action"`
	Cid           string `json:"cid" cborgen:"cid"`
	Path          string `json:"path" cborgen:"path"`
}
