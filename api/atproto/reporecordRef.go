package atproto

// schema: com.atproto.repo.recordRef

func init() {
}

type RepoRecordRef struct {
	LexiconTypeID string  `json:"$type,omitempty"`
	Cid           *string `json:"cid,omitempty" cborgen:"cid"`
	Uri           string  `json:"uri" cborgen:"uri"`
}
